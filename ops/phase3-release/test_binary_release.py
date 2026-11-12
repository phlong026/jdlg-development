"""Isolated deployment state-machine checks; never contact production."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

# This directory contains inspect.py; prevent it shadowing Python's stdlib.
HERE = str(Path(__file__).resolve().parent)
original_path = sys.path[:]
sys.path = [item for item in sys.path if str(Path(item or '.').resolve()) != HERE]
from unittest import mock
sys.path = original_path

spec = importlib.util.spec_from_file_location('binary_release', Path(__file__).with_name('binary_release.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


def elf(payload):
    header = bytearray(64)
    header[:6] = b'\x7fELF\x02\x01'
    header[18:20] = b'\x3e\x00'
    return bytes(header) + payload


class BinaryReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.binary = self.root / 'app' / 'applinux'
        self.binary.parent.mkdir()
        self.old, self.new = elf(b'old-version'), elf(b'new-version')
        self.binary.write_bytes(self.old)
        self.binary.chmod(0o755)
        (self.binary.parent / '.env').write_bytes(b'SERVER_PORT=8080\n')
        (self.binary.parent / '.env.local').write_bytes(b'PROJECT_NOTES_ENABLED=true\n')
        self.candidate = self.root / 'candidate'
        self.candidate.write_bytes(self.new)
        self.backup = self.root / 'private'
        self.env = {'SERVER_PORT': '8080', 'SYNTHETIC_SECRET': 'test-only', 'PROJECT_NOTES_ENABLED': 'true'}
        self.argv = ['./applinux', '--synthetic-argument']
        self.original = dict(pid=111, start_time='10', exe=str(self.binary), cwd=str(self.binary.parent), uid=os.geteuid(), gid=os.getegid(), exe_inode=123, exe_device=1)
        self.processes = [self.original]
        self.started = []
        for name, value in [('BINARY', self.binary), ('identity', mock.Mock(side_effect=lambda pid: next(item for item in self.processes if item['pid'] == pid))),
                            ('find_processes', mock.Mock(side_effect=lambda: self.processes[:])),
                            ('process_environment', mock.Mock(return_value=self.env)), ('process_arguments', mock.Mock(return_value=self.argv)),
                            ('process_digest', mock.Mock(side_effect=lambda _: r.digest(self.binary.read_bytes())))]:
            patch = mock.patch.object(r, name, value)
            patch.start(); self.addCleanup(patch.stop)
        for owner, name in [(r.os, 'pidfd_open'), (r.signal, 'pidfd_send_signal')]:
            patch = mock.patch.object(owner, name, mock.Mock(), create=True)
            patch.start(); self.addCleanup(patch.stop)

    def prepare(self):
        return r.prepare(self.backup, self.candidate, r.digest(self.new), 111, r.digest(self.old))

    def stop(self, expected):
        if self.processes != [expected]:
            raise RuntimeError('unexpected process')
        self.processes = []

    def start(self, backup, state, sha):
        self.assertEqual(self.processes, [])
        r.verify_binary(sha)
        self.assertEqual(json.loads((backup / 'runtime-env.json').read_text()), self.env)
        self.assertEqual(json.loads((backup / 'cmdline.json').read_text()), self.argv)
        current = dict(self.original, pid=200 + len(self.started), start_time=str(20 + len(self.started)))
        self.started.append(sha)
        self.processes = [current]
        state['active_process'] = current
        r.save_state(backup, state)
        return current

    def operations(self, health=None, start=None):
        for name, effect in [('stop_process', self.stop), ('start_process', start or self.start), ('wait_healthy', health or (lambda _: None))]:
            patch = mock.patch.object(r, name, side_effect=effect)
            patch.start(); self.addCleanup(patch.stop)

    def test_prepare_records_private_immutable_snapshots(self):
        state = self.prepare()
        self.assertEqual(state['old_sha'], r.digest(self.old))
        self.assertEqual(state['candidate_sha'], r.digest(self.new))
        self.assertEqual((self.backup / 'binary.previous').read_bytes(), self.old)
        self.assertEqual((self.backup / '.env.local.previous').read_bytes(), b'PROJECT_NOTES_ENABLED=true\n')
        self.assertEqual(self.backup.stat().st_mode & 0o777, 0o700)
        for file in self.backup.iterdir():
            self.assertEqual(file.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.binary.read_bytes(), self.old)

    def test_success_and_explicit_rollback_preserve_environment_files(self):
        self.prepare(); self.operations()
        self.assertEqual(r.activate(self.backup)['status'], 'active')
        self.assertEqual(self.binary.read_bytes(), self.new)
        self.assertEqual(r.rollback(self.backup)['status'], 'rolled_back')
        self.assertEqual(self.binary.read_bytes(), self.old)
        self.assertEqual(self.binary.stat().st_mode & 0o777, 0o755)
        self.assertEqual((self.binary.parent / '.env.local').read_bytes(), b'PROJECT_NOTES_ENABLED=true\n')
        self.assertEqual(self.started, [r.digest(self.new), r.digest(self.old)])
        r.rollback(self.backup)
        self.assertEqual(len(self.started), 2)

    def test_candidate_snapshot_tampering_refuses_before_stop(self):
        self.prepare()
        (self.backup / 'binary.candidate').write_bytes(elf(b'tampered'))
        with mock.patch.object(r, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'integrity'):
                r.activate(self.backup)
            stop.assert_not_called()

    def test_pid_replacement_refuses_before_stop(self):
        self.prepare()
        self.processes = [dict(self.original, start_time='11')]
        with mock.patch.object(r, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'identity'):
                r.activate(self.backup)
            stop.assert_not_called()

    def test_configuration_drift_refuses_before_stop(self):
        self.prepare()
        (self.binary.parent / '.env.local').write_bytes(b'new-operator-change')
        with mock.patch.object(r, 'stop_process') as stop:
            with self.assertRaisesRegex(RuntimeError, 'configuration changed'):
                r.activate(self.backup)
            stop.assert_not_called()

    def test_health_failure_restores_original_binary_and_runtime(self):
        self.prepare()
        def healthy(_):
            if self.binary.read_bytes() == self.new:
                raise RuntimeError('unhealthy candidate')
        self.operations(health=healthy)
        with self.assertRaisesRegex(RuntimeError, 'original binary and runtime restored'):
            r.activate(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.old)
        self.assertEqual(r.load_state(self.backup)['status'], 'rolled_back')
        self.assertEqual(self.started, [r.digest(self.new), r.digest(self.old)])

    def test_start_failure_restores_original(self):
        self.prepare()
        def start(backup, state, sha):
            if sha == r.digest(self.new):
                raise OSError('exec format failure')
            return self.start(backup, state, sha)
        self.operations(start=start)
        with self.assertRaisesRegex(RuntimeError, 'restored'):
            r.activate(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.old)
        self.assertEqual(len(self.processes), 1)

    def test_failure_after_atomic_replace_restores_original(self):
        self.prepare(); self.operations()
        original_write = r.atomic_write
        def write(path, data, *args, **kwargs):
            original_write(path, data, *args, **kwargs)
            if path == self.binary and data == self.new:
                raise OSError('directory fsync failure after replacement')
        with mock.patch.object(r, 'atomic_write', side_effect=write):
            with self.assertRaisesRegex(RuntimeError, 'restored'):
                r.activate(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.old)

    def test_manual_rollback_recovers_interrupted_installation(self):
        state = self.prepare(); self.operations()
        self.processes = []
        self.binary.write_bytes(self.new)
        state['status'] = 'installing'
        r.save_state(self.backup, state)
        self.assertEqual(r.rollback(self.backup)['status'], 'rolled_back')
        self.assertEqual(self.binary.read_bytes(), self.old)

    def test_rollback_refuses_unknown_process_without_overwriting(self):
        state = self.prepare(); self.operations()
        state['status'] = 'starting'; r.save_state(self.backup, state)
        self.processes = [dict(self.original, pid=999)]
        self.binary.write_bytes(self.new)
        with self.assertRaisesRegex(RuntimeError, 'unknown service'):
            r.rollback(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.new)

    def test_wrong_candidate_sha_or_architecture_never_stops_service(self):
        with self.assertRaises(RuntimeError):
            r.prepare(self.backup, self.candidate, '0' * 64, 111, r.digest(self.old))
        bad = bytearray(self.new); bad[18:20] = b'\xb7\x00'; self.candidate.write_bytes(bad)
        with self.assertRaisesRegex(RuntimeError, 'architecture'):
            r.prepare(self.backup, self.candidate, r.digest(bad), 111, r.digest(self.old))
        self.assertFalse(self.backup.exists())

    def test_candidate_changed_during_stop_is_not_installed(self):
        self.prepare()
        def stop(expected):
            self.stop(expected)
            (self.backup / 'binary.candidate').write_bytes(elf(b'tampered'))
        self.operations()
        with mock.patch.object(r, 'stop_process', side_effect=stop):
            with self.assertRaisesRegex(RuntimeError, 'restored'):
                r.activate(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.old)
        self.assertEqual(self.started, [r.digest(self.old)])

    def test_timeout_stopping_keeps_healthy_original_running(self):
        self.prepare(); self.operations()
        with mock.patch.object(r, 'stop_process', side_effect=RuntimeError('did not stop')):
            with self.assertRaisesRegex(RuntimeError, 'restored'):
                r.activate(self.backup)
        self.assertEqual(self.binary.read_bytes(), self.old)
        self.assertEqual(self.processes, [self.original])
        self.assertEqual(self.started, [])

    def test_real_start_function_passes_exact_argv_cwd_and_environment(self):
        state = self.prepare()
        self.processes = []
        child = mock.Mock(pid=222)
        child.poll.return_value = None
        running = dict(self.original, pid=222)
        def spawn(*args, **kwargs):
            self.processes = [running]
            return child
        with mock.patch.object(r.subprocess, 'Popen', side_effect=spawn) as popen, \
             mock.patch.object(r.os, 'pidfd_open', return_value=os.open(os.devnull, os.O_RDONLY)):
            result = r.start_process(self.backup, state, r.digest(self.old))
        self.assertEqual(result, running)
        args, kwargs = popen.call_args
        self.assertEqual(args, (self.argv,))
        self.assertEqual(kwargs['env'], self.env)
        self.assertEqual(kwargs['cwd'], str(self.binary.parent))
        self.assertEqual(kwargs['executable'], str(self.binary))
        self.assertTrue(kwargs['start_new_session'])
        self.assertTrue(kwargs['close_fds'])
        self.assertEqual(r.load_state(self.backup)['active_process'], running)

    def test_health_requires_both_public_and_authenticated_boundary(self):
        with mock.patch.object(r, 'health_status', side_effect=[200, 200, 200, 401]) as status, \
             mock.patch.object(r.time, 'sleep'):
            r.wait_healthy(self.original)
        self.assertEqual(status.call_count, 4)

    def test_pidfd_signal_checks_identity_again_before_signalling(self):
        expected = self.original
        with mock.patch.object(r, 'identity', side_effect=[expected, dict(expected, start_time='different')]), \
             mock.patch.object(r.os, 'pidfd_open', return_value=42), mock.patch.object(r.os, 'close'), \
             mock.patch.object(r.signal, 'pidfd_send_signal') as send:
            with self.assertRaisesRegex(RuntimeError, 'changed before signal'):
                r.stop_process(expected)
            send.assert_not_called()


if __name__ == '__main__':
    unittest.main()
