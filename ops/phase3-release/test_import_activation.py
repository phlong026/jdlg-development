#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))
import import_activation as tool


def sha(data):
    return hashlib.sha256(data).hexdigest()


class ImportActivationTest(unittest.TestCase):
    def setUp(self):
        # binary_release deliberately rejects paths containing symlink components;
        # macOS /var points at /private/var, so keep fixtures below the checkout.
        self.temp = tempfile.TemporaryDirectory(dir=Path.cwd())
        self.root = Path(self.temp.name)
        self.app = self.root / 'app'
        self.app.mkdir()
        self.binary = self.app / 'applinux'
        self.binary.write_bytes(b'ELF-test-binary')
        self.env_file = self.app / '.env.local'
        self.original_env = b'# keep this comment\nUNCHANGED=value\n'
        self.env_file.write_bytes(self.original_env)
        os.chmod(self.env_file, 0o600)
        self.capture = self.root / 'main-service.env'
        self.secret = 'test-secret-never-output'
        self.capture.write_text(
            'CONTENT_IMPORT_CAPTURE_URL=https://127.0.0.1:9443/v1/capture\n'
            'CONTENT_IMPORT_CAPTURE_HEALTH_URL=https://127.0.0.1:9443/health/ready\n'
            'CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS=127.0.0.1:9443\n'
            f'CONTENT_IMPORT_CAPTURE_SECRET={self.secret}\n'
        )
        os.chmod(self.capture, 0o600)
        self.backup = self.root / 'backup'
        self.old_sha = sha(self.binary.read_bytes())
        self.runtime = {'SERVER_PORT': '8080', 'SAFE': 'preserved'}
        self.arguments = [str(self.binary), '--serve']
        self.original_process = {
            'pid': 1234,
            'start_time': '10',
            'exe': str(self.binary),
            'cwd': str(self.app),
            'uid': os.geteuid(),
            'gid': os.getegid(),
            'exe_inode': 88,
            'exe_device': 77,
        }
        self.active_process = dict(self.original_process, pid=2345, start_time='11')
        patches = [
            mock.patch.object(tool, 'BINARY', self.binary),
            mock.patch.object(tool, 'ENV_FILE', self.env_file),
            mock.patch.object(tool, 'CAPTURE_ENV', self.capture),
            mock.patch.object(tool.release, 'identity', return_value=self.original_process),
            mock.patch.object(tool.release, 'find_processes', return_value=[self.original_process]),
            mock.patch.object(tool.release, 'process_digest', return_value=self.old_sha),
            mock.patch.object(tool.release, 'process_environment', return_value=dict(self.runtime)),
            mock.patch.object(tool.release, 'process_arguments', return_value=list(self.arguments)),
        ]
        self.mocks = [patch.start() for patch in patches]
        self.addCleanup(lambda: [patch.stop() for patch in reversed(patches)])

    def tearDown(self):
        self.temp.cleanup()

    def prepare(self):
        return tool.prepare(self.backup, 1234, self.old_sha, schema_verified=True)

    def test_prepare_requires_schema_verification(self):
        with self.assertRaisesRegex(RuntimeError, 'schema verification'):
            tool.prepare(self.backup, 1234, self.old_sha, schema_verified=False)
        self.assertFalse(self.backup.exists())

    def test_prepare_snapshots_private_files_without_capture_secret(self):
        state = self.prepare()
        self.assertEqual('prepared', state['status'])
        self.assertEqual(0, stat.S_IMODE(self.backup.stat().st_mode) & 0o077)
        self.assertEqual(self.original_env, (self.backup / 'env.previous').read_bytes())
        self.assertEqual(self.runtime, json.loads((self.backup / 'runtime-env.json').read_text()))
        self.assertFalse((self.backup / 'capture-source.env').exists())
        combined = b''.join(path.read_bytes() for path in self.backup.iterdir())
        self.assertNotIn(self.secret.encode(), combined)

    def test_prepare_refuses_conflicting_authoritative_process_environment(self):
        tool.release.process_environment.return_value = dict(
            self.runtime, CONTENT_IMPORT_CAPTURE_SECRET='different-secret'
        )
        with self.assertRaisesRegex(RuntimeError, 'process environment conflicts'):
            self.prepare()
        self.assertFalse(self.backup.exists())

    def test_prepare_never_overwrites_existing_backup_directory(self):
        self.backup.mkdir()
        with self.assertRaisesRegex(RuntimeError, 'new private directory'):
            self.prepare()

    def test_activate_preserves_unrelated_config_and_does_not_expose_secret(self):
        self.prepare()
        tool.release.find_processes.return_value = [self.original_process]
        started_with = []

        def start(backup, state, environment):
            started_with.append(dict(environment))
            return self.active_process

        with mock.patch.object(tool.release, 'stop_process') as stop, \
                mock.patch.object(tool, '_start_process', side_effect=start), \
                mock.patch.object(tool.release, 'wait_healthy'), \
                mock.patch.object(tool, '_verify_started_import_environment'):
            state = tool.activate(self.backup)
        self.assertEqual('active', state['status'])
        stop.assert_called_once_with(self.original_process)
        contents = self.env_file.read_text()
        self.assertIn('# keep this comment\nUNCHANGED=value\n', contents)
        self.assertIn('CONTENT_IMPORT_ENABLED=true\n', contents)
        self.assertIn(f'CONTENT_IMPORT_CAPTURE_SECRET={self.secret}\n', contents)
        self.assertEqual(0o600, stat.S_IMODE(self.env_file.stat().st_mode))
        self.assertNotIn('CONTENT_IMPORT_CAPTURE_SECRET', started_with[0])
        self.assertNotIn('CONTENT_IMPORT_ENABLED', started_with[0])
        self.assertNotIn(self.secret, json.dumps(state))

    def test_effective_verification_uses_local_file_when_proc_env_has_no_import_keys(self):
        _, desired = tool._capture_values()
        self.env_file.write_bytes(tool._render_env(self.original_env, desired))
        tool.release.process_environment.return_value = dict(self.runtime)
        tool._verify_started_import_environment(self.active_process, self.runtime, desired)

    def test_activate_refuses_capture_source_drift_before_edit_or_restart(self):
        self.prepare()
        self.capture.write_text(self.capture.read_text().replace(self.secret, 'changed'))
        with mock.patch.object(tool.release, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'capture source changed'):
                tool.activate(self.backup)
        stop.assert_not_called()
        self.assertEqual(self.original_env, self.env_file.read_bytes())

    def test_activate_rejects_capture_drift_during_desired_read(self):
        self.prepare()
        read_capture = tool._capture_values

        def drift_then_read():
            self.capture.write_text(self.capture.read_text().replace(self.secret, 'changed-during-read'))
            return read_capture()

        with mock.patch.object(tool, '_capture_values', side_effect=drift_then_read), \
                mock.patch.object(tool.release, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'reviewed values changed'):
                tool.activate(self.backup)
        stop.assert_not_called()
        self.assertEqual(self.original_env, self.env_file.read_bytes())

    def test_activate_preserves_external_env_edit_after_intent_save(self):
        self.prepare()
        external = b'EXTERNAL=operator-change\n'
        save_state = tool._save_state

        def save_then_edit(backup, state):
            save_state(backup, state)
            if state['status'] == 'configuring':
                self.env_file.write_bytes(external)

        with mock.patch.object(tool, '_save_state', side_effect=save_then_edit), \
                mock.patch.object(tool.release, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'configuration changed after prepare'):
                tool.activate(self.backup)
        stop.assert_not_called()
        self.assertEqual(external, self.env_file.read_bytes())

    def test_activate_preserves_external_capture_edit_after_intent_save(self):
        self.prepare()
        external = self.capture.read_text().replace(self.secret, 'operator-rotated')
        save_state = tool._save_state

        def save_then_edit(backup, state):
            save_state(backup, state)
            if state['status'] == 'configuring':
                self.capture.write_text(external)

        with mock.patch.object(tool, '_save_state', side_effect=save_then_edit), \
                mock.patch.object(tool.release, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'capture source changed'):
                tool.activate(self.backup)
        stop.assert_not_called()
        self.assertEqual(external, self.capture.read_text())
        self.assertEqual(self.original_env, self.env_file.read_bytes())

    def test_failed_activation_restores_config_before_original_runtime(self):
        self.prepare()
        starts = []

        def start(backup, state, environment):
            starts.append((self.env_file.read_bytes(), dict(environment)))
            state['active_process'] = self.active_process
            return self.active_process

        tool.release.find_processes.reset_mock()
        tool.release.find_processes.side_effect = [
            [self.original_process],
            [self.original_process],
            [self.active_process],
        ]
        with mock.patch.object(tool.release, 'stop_process'), \
                mock.patch.object(tool, '_start_process', side_effect=start), \
                mock.patch.object(tool.release, 'wait_healthy', side_effect=[RuntimeError('bad health'), None]), \
                mock.patch.object(tool, '_verify_started_import_environment'):
            with self.assertRaisesRegex(RuntimeError, 'original configuration and runtime restored'):
                tool.activate(self.backup)
        self.assertEqual(self.original_env, self.env_file.read_bytes())
        self.assertEqual(2, len(starts))
        self.assertNotEqual(self.original_env, starts[0][0])
        self.assertEqual(self.original_env, starts[1][0])
        self.assertEqual(self.runtime, starts[1][1])
        state = json.loads((self.backup / 'state.json').read_text())
        self.assertEqual('rolled_back', state['status'])

    def test_rollback_restores_config_before_original_runtime(self):
        self.prepare()
        with mock.patch.object(tool.release, 'stop_process'), \
                mock.patch.object(tool, '_start_process', return_value=self.active_process), \
                mock.patch.object(tool.release, 'wait_healthy'), \
                mock.patch.object(tool, '_verify_started_import_environment'):
            tool.activate(self.backup)

        observed = []

        def start(backup, state, environment):
            observed.append((self.env_file.read_bytes(), dict(environment)))
            return self.original_process

        tool.release.find_processes.return_value = [self.active_process]
        with mock.patch.object(tool.release, 'stop_process') as stop, \
                mock.patch.object(tool, '_start_process', side_effect=start), \
                mock.patch.object(tool.release, 'wait_healthy'):
            state = tool.rollback(self.backup)
        stop.assert_called_once_with(self.active_process)
        self.assertEqual('rolled_back', state['status'])
        self.assertEqual([(self.original_env, self.runtime)], observed)

    def test_rollback_refuses_replaced_process_without_touching_config(self):
        self.prepare()
        with mock.patch.object(tool.release, 'stop_process'), \
                mock.patch.object(tool, '_start_process', return_value=self.active_process), \
                mock.patch.object(tool.release, 'wait_healthy'), \
                mock.patch.object(tool, '_verify_started_import_environment'):
            tool.activate(self.backup)
        active_config = self.env_file.read_bytes()
        replacement = dict(self.active_process, start_time='replacement')
        tool.release.find_processes.return_value = [replacement]
        with self.assertRaisesRegex(RuntimeError, 'process was replaced'):
            tool.rollback(self.backup)
        self.assertEqual(active_config, self.env_file.read_bytes())

    def test_rollback_recovers_if_process_died_after_config_write_before_next_state_save(self):
        state = self.prepare()
        _, desired = tool._capture_values()
        active_config = tool._render_env(self.original_env, desired)
        state.update(status='configuring', env_active_sha=sha(active_config))
        tool._save_state(self.backup, state)
        tool.release.atomic_write(self.env_file, active_config, mode=0o600)

        tool.release.find_processes.return_value = [self.original_process]
        with mock.patch.object(tool.release, 'wait_healthy'):
            restored = tool.rollback(self.backup)
        self.assertEqual('rolled_back', restored['status'])
        self.assertEqual(self.original_env, self.env_file.read_bytes())

    def test_rollback_refuses_capture_source_drift_without_touching_config(self):
        state = self.prepare()
        _, desired = tool._capture_values()
        active_config = tool._render_env(self.original_env, desired)
        state.update(status='configuring', env_active_sha=sha(active_config))
        tool._save_state(self.backup, state)
        tool.release.atomic_write(self.env_file, active_config, mode=0o600)
        self.capture.write_text(self.capture.read_text().replace(self.secret, 'rotated'))

        with self.assertRaisesRegex(RuntimeError, 'capture source changed'):
            tool.rollback(self.backup)
        self.assertEqual(active_config, self.env_file.read_bytes())


if __name__ == '__main__':
    unittest.main()
