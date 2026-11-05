#!/usr/bin/env python3
"""Safely enable or roll back content-import configuration without DB changes.

prepare --backup NEW_PRIVATE_DIRECTORY --expected-pid PID
        --expected-old-sha SHA256 --schema-verified
activate --backup DIRECTORY
rollback --backup DIRECTORY

The operator must verify the content_import_jobs schema separately before
prepare. This tool never migrates or deletes database or media data.
"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import select
import signal
import stat
import subprocess
import time
import traceback

import binary_release as release


BINARY = Path('/www/wwwroot/jiadilingguang/api/applinux')
ENV_FILE = BINARY.parent / '.env.local'
CAPTURE_ENV = Path('/opt/jdlg-content-capture/main-service.env')
LOCK_FILE = BINARY.parent / '.binary-release.lock'
CAPTURE_KEYS = (
    'CONTENT_IMPORT_CAPTURE_URL',
    'CONTENT_IMPORT_CAPTURE_HEALTH_URL',
    'CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS',
    'CONTENT_IMPORT_CAPTURE_SECRET',
)
IMPORT_KEYS = ('CONTENT_IMPORT_ENABLED',) + CAPTURE_KEYS


def _valid_env_key(key):
    return bool(key) and all(
        char == '_' or char.isascii() and char.isalpha()
        or index > 0 and char.isascii() and char.isdigit()
        for index, char in enumerate(key)
    )


def _read_env(path):
    raw = Path(path).read_bytes()
    try:
        text = raw.decode('utf-8')
    except UnicodeDecodeError as error:
        raise RuntimeError('environment file must be UTF-8') from error
    values = {}
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith('#'):
            continue
        if '=' not in line:
            raise RuntimeError('environment file contains an invalid assignment')
        key, value = line.split('=', 1)
        key = key.strip()
        value = value.strip()
        if not _valid_env_key(key) or key in values:
            raise RuntimeError('environment file contains invalid or duplicate keys')
        if value and value[0] in "'\"":
            if len(value) < 2 or value[-1] != value[0]:
                raise RuntimeError('environment file contains an unclosed quote')
            value = value[1:-1]
        if '\r' in value or '\n' in value:
            raise RuntimeError('environment value contains a newline')
        values[key] = value
    return raw, values


def _capture_values():
    info = release.regular(CAPTURE_ENV)
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError('capture source must be private and owned by operator')
    raw, parsed = _read_env(CAPTURE_ENV)
    values = {}
    for key in CAPTURE_KEYS:
        value = parsed.get(key, '')
        if not value or value != value.strip() or value[0] in "'\"":
            raise RuntimeError('capture source is missing a safe required value')
        values[key] = value
    if not values['CONTENT_IMPORT_CAPTURE_URL'].startswith('https://') or not values['CONTENT_IMPORT_CAPTURE_HEALTH_URL'].startswith('https://'):
        raise RuntimeError('capture endpoints must use HTTPS')
    return raw, dict(CONTENT_IMPORT_ENABLED='true', **values)


def _render_env(original, desired):
    try:
        text = original.decode('utf-8')
    except UnicodeDecodeError as error:
        raise RuntimeError('environment file must be UTF-8') from error
    seen = set()
    output = []
    for line in text.splitlines(keepends=True):
        body = line.rstrip('\r\n')
        if '=' not in body or body.lstrip().startswith('#'):
            output.append(line)
            continue
        key = body.split('=', 1)[0].strip()
        if key not in desired:
            output.append(line)
            continue
        if key in seen:
            raise RuntimeError('target environment contains duplicate import keys')
        seen.add(key)
        output.append(f'{key}={desired[key]}\n')
    if output and not output[-1].endswith(('\n', '\r')):
        output[-1] += '\n'
    for key in IMPORT_KEYS:
        if key not in seen:
            output.append(f'{key}={desired[key]}\n')
    return ''.join(output).encode('utf-8')


def _new_private_directory(backup):
    if (backup.exists() or backup.is_symlink() or not backup.is_absolute()
            or backup.parent.resolve() != backup.parent
            or BINARY.parent in backup.parents or Path('/www/wwwroot') in backup.parents):
        raise RuntimeError('backup must be a new private directory outside application directory')
    backup.mkdir(mode=0o700)
    os.chmod(backup, 0o700)


def _private_directory(backup):
    if not backup.is_absolute() or backup.resolve() != backup or backup.is_symlink():
        raise RuntimeError('invalid backup path')
    info = backup.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError('backup directory must be private and owned by operator')


def _save_state(backup, state):
    release.atomic_write(
        backup / 'state.json',
        json.dumps(state, ensure_ascii=True, indent=2).encode('utf-8'),
    )


def _load_state(backup):
    _private_directory(backup)
    state_info = release.regular(backup / 'state.json')
    if state_info.st_uid != os.geteuid() or state_info.st_mode & 0o077:
        raise RuntimeError('state must be private')
    state = json.loads((backup / 'state.json').read_text())
    if state.get('binary') != str(BINARY) or state.get('env_file') != str(ENV_FILE) or state.get('capture_source') != str(CAPTURE_ENV):
        raise RuntimeError('snapshot belongs to another deployment')
    for name, expected_hash in state['snapshot_hashes'].items():
        if Path(name).name != name:
            raise RuntimeError('invalid snapshot filename')
        info = release.regular(backup / name)
        if info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise RuntimeError('snapshot permissions changed')
        if release.digest((backup / name).read_bytes()) != expected_hash:
            raise RuntimeError('snapshot integrity changed')
    return state


def _verify_binary(expected_sha):
    release.regular(BINARY)
    if release.digest(BINARY.read_bytes()) != expected_sha:
        raise RuntimeError('live binary changed')


def _verify_process(expected, binary_sha):
    if release.find_processes() != [expected] or release.process_digest(expected['pid']) != binary_sha:
        raise RuntimeError('service process identity or running executable changed')


def _verify_original_runtime(backup, state):
    expected = state['original_process']
    _verify_process(expected, state['binary_sha'])
    if release.process_environment(expected['pid']) != json.loads((backup / 'runtime-env.json').read_text()):
        raise RuntimeError('runtime environment changed')
    if release.process_arguments(expected['pid']) != json.loads((backup / 'cmdline.json').read_text()):
        raise RuntimeError('runtime arguments changed')


def _verify_original_config(backup, state):
    info = release.metadata(ENV_FILE)
    if info != state['env_metadata'] or release.digest(ENV_FILE.read_bytes()) != state['env_original_sha']:
        raise RuntimeError('environment configuration changed after prepare')
    if (backup / 'env.previous').read_bytes() != ENV_FILE.read_bytes():
        raise RuntimeError('environment snapshot no longer matches live configuration')


def _verify_capture_source(state):
    info = release.metadata(CAPTURE_ENV)
    if info != state['capture_metadata'] or release.digest(CAPTURE_ENV.read_bytes()) != state['capture_source_sha']:
        raise RuntimeError('capture source changed after prepare')


def _reviewed_capture_values(state):
    raw, desired = _capture_values()
    hashes = {key: release.digest(value.encode('utf-8')) for key, value in desired.items()}
    if release.digest(raw) != state['capture_source_sha'] or hashes != state['desired_value_hashes']:
        raise RuntimeError('capture source or reviewed values changed after prepare')
    return desired


def _start_process(backup, state, environment):
    _verify_binary(state['binary_sha'])
    if release.find_processes():
        raise RuntimeError('refusing duplicate service process')
    arguments = json.loads((backup / 'cmdline.json').read_text())
    fd = os.open(backup / 'process.log', os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'ab') as output:
        os.fchmod(output.fileno(), 0o600)
        process = subprocess.Popen(
            arguments,
            executable=str(BINARY),
            cwd=state['original_process']['cwd'],
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=output,
            start_new_session=True,
            close_fds=True,
        )
    owned_fd = os.pidfd_open(process.pid)
    try:
        for _ in range(20):
            if process.poll() is not None:
                raise RuntimeError('service exited immediately; inspect private process log')
            try:
                current = release.identity(process.pid)
                if current['exe'] == str(BINARY):
                    if release.find_processes() != [current] or release.process_digest(current['pid']) != state['binary_sha']:
                        raise RuntimeError('started service process does not match reviewed binary')
                    if release.process_arguments(current['pid']) != arguments:
                        raise RuntimeError('started service arguments changed')
                    state['active_process'] = current
                    _save_state(backup, state)
                    return current
            except (FileNotFoundError, ProcessLookupError):
                pass
            time.sleep(0.05)
        raise RuntimeError('unable to identify newly started process')
    except BaseException:
        if process.poll() is None:
            signal.pidfd_send_signal(owned_fd, signal.SIGTERM)
            if not select.select([owned_fd], [], [], 15)[0]:
                raise RuntimeError('new service did not stop; no force kill attempted')
            process.wait(timeout=15)
        raise
    finally:
        os.close(owned_fd)


def _verify_started_import_environment(process, original_runtime, desired):
    environment = release.process_environment(process['pid'])
    if environment != original_runtime:
        raise RuntimeError('started service environment differs from runtime snapshot')
    _, local_values = _read_env(ENV_FILE)
    effective = {
        key: environment[key] if key in environment else local_values.get(key)
        for key in IMPORT_KEYS
    }
    if effective != desired:
        raise RuntimeError('started service effective import configuration is incorrect')


def prepare(backup, expected_pid, expected_sha, schema_verified=False):
    if not schema_verified:
        raise RuntimeError('explicit schema verification is required')
    if len(expected_sha) != 64 or any(char not in '0123456789abcdef' for char in expected_sha):
        raise RuntimeError('explicit reviewed binary SHA256 required')
    binary_info = release.metadata(BINARY)
    _verify_binary(expected_sha)
    env_info = release.metadata(ENV_FILE)
    if env_info['uid'] != os.geteuid() or env_info['mode'] & 0o077:
        raise RuntimeError('environment configuration must be private and owned by operator')
    env_bytes, _ = _read_env(ENV_FILE)
    capture_bytes, desired = _capture_values()
    current = release.identity(expected_pid)
    if (current['exe'] != str(BINARY) or current['cwd'] != str(BINARY.parent)
            or current['uid'] != os.geteuid() or current['gid'] != os.getegid()):
        raise RuntimeError('process does not match reviewed deployment identity')
    environment = release.process_environment(expected_pid)
    arguments = release.process_arguments(expected_pid)
    if not arguments or environment.get('SERVER_PORT', '8080') != '8080':
        raise RuntimeError('unexpected runtime command or port')
    if environment.get('JDLG_LOCAL_ENV_FILE', str(ENV_FILE)) != str(ENV_FILE):
        raise RuntimeError('service uses another local environment file')
    if any(key in environment and environment[key] != desired[key] for key in IMPORT_KEYS):
        raise RuntimeError('process environment conflicts with desired import configuration')
    if release.find_processes() != [current] or release.process_digest(expected_pid) != expected_sha:
        raise RuntimeError('service process identity or running executable changed')
    _new_private_directory(backup)
    snapshots = {
        'env.previous': env_bytes,
        'runtime-env.json': json.dumps(environment, ensure_ascii=True).encode('utf-8'),
        'cmdline.json': json.dumps(arguments, ensure_ascii=True).encode('utf-8'),
    }
    for name, contents in snapshots.items():
        release.atomic_write(backup / name, contents)
    state = {
        'status': 'prepared',
        'binary': str(BINARY),
        'binary_sha': expected_sha,
        'binary_metadata': binary_info,
        'env_file': str(ENV_FILE),
        'env_metadata': env_info,
        'env_original_sha': release.digest(env_bytes),
        'env_active_sha': None,
        'capture_source': str(CAPTURE_ENV),
        'capture_metadata': release.metadata(CAPTURE_ENV),
        'capture_source_sha': release.digest(capture_bytes),
        'desired_value_hashes': {key: release.digest(value.encode('utf-8')) for key, value in desired.items()},
        'schema_verified': True,
        'effective_config_verified': False,
        'original_process': current,
        'active_process': None,
        'snapshot_hashes': {name: release.digest(contents) for name, contents in snapshots.items()},
    }
    _verify_binary(expected_sha)
    _verify_original_config(backup, state)
    _verify_capture_source(state)
    _verify_original_runtime(backup, state)
    _save_state(backup, state)
    return state


def _restore(backup, state):
    current = release.find_processes()
    expected = state.get('active_process') or state['original_process']
    if current and current != [expected]:
        raise RuntimeError('unknown service process blocks rollback')
    release.regular(ENV_FILE)
    live_hash = release.digest(ENV_FILE.read_bytes())
    allowed = {state['env_original_sha'], state.get('env_active_sha')}
    if live_hash not in allowed:
        raise RuntimeError('unknown environment configuration blocks rollback')
    previous = (backup / 'env.previous').read_bytes()
    release.atomic_write(ENV_FILE, previous, **state['env_metadata'])
    state['status'] = 'restoring'
    _save_state(backup, state)

    if current:
        if expected == state['original_process']:
            _verify_original_runtime(backup, state)
            release.wait_healthy(expected)
            state.update(status='rolled_back', active_process=expected, effective_config_verified=False)
            _save_state(backup, state)
            return state
        release.stop_process(expected)

    environment = json.loads((backup / 'runtime-env.json').read_text())
    restored = _start_process(backup, state, environment)
    release.wait_healthy(restored)
    actual = release.process_environment(restored['pid'])
    if any(actual.get(key) != environment.get(key) for key in IMPORT_KEYS):
        raise RuntimeError('restored service import environment differs from snapshot')
    state.update(status='rolled_back', active_process=restored, effective_config_verified=False)
    _save_state(backup, state)
    return state


def activate(backup):
    state = _load_state(backup)
    if state['status'] != 'prepared' or not state.get('schema_verified'):
        raise RuntimeError('activation requires a schema-verified prepared state')
    _verify_binary(state['binary_sha'])
    if release.metadata(BINARY) != state['binary_metadata']:
        raise RuntimeError('binary ownership or permissions changed after prepare')
    _verify_original_config(backup, state)
    _verify_capture_source(state)
    _verify_original_runtime(backup, state)
    desired = _reviewed_capture_values(state)
    original_runtime = json.loads((backup / 'runtime-env.json').read_text())
    if any(key in original_runtime and original_runtime[key] != desired[key] for key in IMPORT_KEYS):
        raise RuntimeError('process environment conflicts with desired import configuration')
    active_config = _render_env((backup / 'env.previous').read_bytes(), desired)
    state.update(status='configuring', env_active_sha=release.digest(active_config))
    _save_state(backup, state)

    # Re-check every input after persisting intent. Drift here belongs to an
    # external operator, so refuse without invoking restore or overwriting it.
    _verify_original_config(backup, state)
    _verify_capture_source(state)
    _verify_original_runtime(backup, state)
    _reviewed_capture_values(state)
    release.atomic_write(ENV_FILE, active_config, mode=0o600, uid=state['env_metadata']['uid'], gid=state['env_metadata']['gid'])
    active_metadata = dict(mode=0o600, uid=state['env_metadata']['uid'], gid=state['env_metadata']['gid'])
    if release.metadata(ENV_FILE) != active_metadata or release.digest(ENV_FILE.read_bytes()) != state['env_active_sha']:
        raise RuntimeError('environment configuration changed during activation write')

    try:
        state['status'] = 'configured'
        _save_state(backup, state)
        release.stop_process(state['original_process'])
        state['status'] = 'starting'
        _save_state(backup, state)
        started = _start_process(backup, state, original_runtime)
        release.wait_healthy(started)
        _verify_started_import_environment(started, original_runtime, desired)
        if release.digest(ENV_FILE.read_bytes()) != state['env_active_sha']:
            raise RuntimeError('active environment configuration changed during restart')
        state.update(status='active', active_process=started, effective_config_verified=True)
        _save_state(backup, state)
        return state
    except BaseException:
        _restore(backup, state)
        raise RuntimeError('activation failed; original configuration and runtime restored') from None


def rollback(backup):
    state = _load_state(backup)
    if state['status'] == 'rolled_back':
        _verify_binary(state['binary_sha'])
        _verify_original_config(backup, state)
        _verify_process(state['active_process'], state['binary_sha'])
        release.wait_healthy(state['active_process'])
        return state
    if state['status'] not in ('configuring', 'configured', 'starting', 'active', 'restoring'):
        raise RuntimeError('rollback requires an attempted activation')
    _verify_capture_source(state)
    _verify_binary(state['binary_sha'])
    if state.get('active_process') and release.find_processes() not in ([], [state['active_process']]):
        raise RuntimeError('service process was replaced after activation')
    return _restore(backup, state)


def _summary(state):
    process = state.get('active_process') or state['original_process']
    return {
        'status': state['status'],
        'binary_sha': state['binary_sha'],
        'env_sha': state.get('env_active_sha') if state['status'] == 'active' else state['env_original_sha'],
        'pid': process['pid'],
        'schema_verified': bool(state.get('schema_verified')),
        'effective_config_verified': bool(state.get('effective_config_verified')),
        'capture_source_unchanged': True,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('prepare', 'activate', 'rollback'))
    parser.add_argument('--backup', required=True, type=Path)
    parser.add_argument('--expected-pid', type=int)
    parser.add_argument('--expected-old-sha')
    parser.add_argument('--schema-verified', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if os.geteuid() != 0:
            raise RuntimeError('root operator required')
        lock = os.open(LOCK_FILE, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if args.action == 'prepare':
                if args.expected_pid is None or args.expected_old_sha is None:
                    raise RuntimeError('expected PID and reviewed binary SHA are required')
                state = prepare(args.backup, args.expected_pid, args.expected_old_sha, args.schema_verified)
            else:
                state = {'activate': activate, 'rollback': rollback}[args.action](args.backup)
        finally:
            os.close(lock)
    except BaseException as error:
        try:
            _private_directory(args.backup)
            fd = os.open(args.backup / 'errors.log', os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'a') as output:
                os.fchmod(output.fileno(), 0o600)
                traceback.print_exc(file=output)
        except (OSError, RuntimeError):
            pass
        print('FAILED: ' + type(error).__name__ + '; inspect private backup state and logs')
        return 1
    print(json.dumps(_summary(state), ensure_ascii=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
