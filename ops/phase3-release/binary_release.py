#!/usr/bin/env python3
"""Binary-only release. No migrations, environment edits, or feature-flag changes.

prepare --candidate FILE --candidate-sha SHA --backup NEW_PRIVATE_DIRECTORY
        [--expected-pid PID --expected-old-sha SHA]
activate --backup DIRECTORY
rollback --backup DIRECTORY

Run as the same root operator as the service. Keep the backup outside the web
root. A failed activation attempts rollback; inspect state.json and private
process/error logs if recovery is blocked by a changed process or configuration.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import select
import signal
import stat
import subprocess
import time
import traceback
import urllib.error
import urllib.request

BINARY = Path('/www/wwwroot/jiadilingguang/api/applinux')
EXPECTED_PID = 1483740
EXPECTED_SHA = '04b378115bc10dd4df25144a409986e361e743597f065ae902c80e34018d277e'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def regular(path):
    path = Path(path)
    if not path.is_absolute() or path.resolve() != path or not stat.S_ISREG(path.lstat().st_mode):
        raise RuntimeError('expected a regular file without symlink components')
    return path.stat()


def metadata(path):
    info = regular(path)
    return dict(mode=stat.S_IMODE(info.st_mode), uid=info.st_uid, gid=info.st_gid)


def atomic_write(path, data, mode=0o600, uid=None, gid=None):
    path = Path(path)
    temp = path.with_name(path.name + '.release-tmp-' + str(os.getpid()))
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            if uid is not None:
                os.fchown(stream.fileno(), uid, gid)
            os.fsync(stream.fileno())
        os.replace(temp, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temp.exists():
            temp.unlink()


def save_state(backup, state):
    atomic_write(backup / 'state.json', json.dumps(state, ensure_ascii=True, indent=2).encode())


def identity(pid):
    proc = Path('/proc') / str(pid)
    fields = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
    info = proc.stat()
    exe = (proc / 'exe').stat()
    return dict(pid=pid, start_time=fields[19], exe=os.readlink(proc / 'exe'), cwd=os.readlink(proc / 'cwd'),
                uid=info.st_uid, gid=info.st_gid, exe_inode=exe.st_ino, exe_device=exe.st_dev)


def process_environment(pid):
    return dict(os.fsdecode(part).split('=', 1) for part in Path(f'/proc/{pid}/environ').read_bytes().split(b'\0') if b'=' in part)


def process_arguments(pid):
    return [os.fsdecode(part) for part in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0') if part]


def process_digest(pid):
    return digest(Path(f'/proc/{pid}/exe').read_bytes())


def find_processes():
    found = []
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit():
            continue
        try:
            if (proc / 'comm').read_text().strip() == BINARY.name or os.readlink(proc / 'exe').removesuffix(' (deleted)') == str(BINARY):
                found.append(identity(int(proc.name)))
        except (FileNotFoundError, ProcessLookupError):
            continue
    return found


def verify_configs(backup, state):
    for name, saved in state['configs'].items():
        path = BINARY.parent / name
        if saved is None:
            if path.exists() or path.is_symlink():
                raise RuntimeError('configuration appeared after prepare')
        elif metadata(path) != saved['metadata'] or digest(path.read_bytes()) != saved['sha']:
            raise RuntimeError('configuration changed after prepare; refusing overwrite')


def verify_binary(expected_sha):
    regular(BINARY)
    if digest(BINARY.read_bytes()) != expected_sha:
        raise RuntimeError('live binary changed')


def verify_runtime(backup, expected, expected_sha):
    if find_processes() != [expected] or process_digest(expected['pid']) != expected_sha:
        raise RuntimeError('service process identity or running executable changed')
    if process_environment(expected['pid']) != json.loads((backup / 'runtime-env.json').read_text()) or process_arguments(expected['pid']) != json.loads((backup / 'cmdline.json').read_text()):
        raise RuntimeError('runtime environment or arguments changed')


def private_directory(backup):
    if not backup.is_absolute() or backup.resolve() != backup or backup.is_symlink():
        raise RuntimeError('invalid backup path')
    info = backup.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError('backup directory must be private and owned by operator')


def load_state(backup):
    private_directory(backup)
    info = regular(backup / 'state.json')
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError('state must be private')
    state = json.loads((backup / 'state.json').read_text())
    if state['binary'] != str(BINARY):
        raise RuntimeError('snapshot belongs to another deployment')
    for name, expected in state['hashes'].items():
        if Path(name).name != name:
            raise RuntimeError('invalid snapshot filename')
        info = regular(backup / name)
        if info.st_uid != os.geteuid() or info.st_mode & 0o077 or digest((backup / name).read_bytes()) != expected:
            raise RuntimeError('snapshot integrity or permissions changed')
    return state


def prepare(backup, candidate, candidate_sha, expected_pid=EXPECTED_PID, expected_sha=EXPECTED_SHA):
    if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
        raise RuntimeError('Linux pidfd support required')
    if len(candidate_sha) != 64 or any(c not in '0123456789abcdef' for c in candidate_sha):
        raise RuntimeError('explicit candidate SHA256 required')
    binary_meta = metadata(BINARY)
    old = BINARY.read_bytes()
    regular(candidate)
    new = candidate.read_bytes()
    if digest(old) != expected_sha or digest(new) != candidate_sha or old == new:
        raise RuntimeError('reviewed binary hashes do not match or candidate is unchanged')
    if len(new) < 64 or old[:4] != b'\x7fELF' or new[:4] != b'\x7fELF' or new[4:6] != old[4:6] or new[18:20] != old[18:20]:
        raise RuntimeError('candidate must be an ELF for the running architecture')
    current = identity(expected_pid)
    if current['exe'] != str(BINARY) or current['cwd'] != str(BINARY.parent) or current['uid'] != os.geteuid() or current['gid'] != os.getegid():
        raise RuntimeError('process does not match reviewed deployment identity')
    environment, arguments = process_environment(expected_pid), process_arguments(expected_pid)
    if not arguments or environment.get('SERVER_PORT', '8080') != '8080' or environment.get('JDLG_LOCAL_ENV_FILE', str(BINARY.parent / '.env.local')) != str(BINARY.parent / '.env.local'):
        raise RuntimeError('unexpected runtime command, port or environment file')
    if backup.exists() or backup.is_symlink() or not backup.is_absolute() or backup.parent.resolve() != backup.parent or BINARY.parent in backup.parents or Path('/www/wwwroot') in backup.parents:
        raise RuntimeError('backup must be a new private directory outside application directory')
    snapshots = {'binary.previous': old, 'binary.candidate': new, 'runtime-env.json': json.dumps(environment, ensure_ascii=True).encode(), 'cmdline.json': json.dumps(arguments, ensure_ascii=True).encode()}
    configs = {}
    for name in ('.env', '.env.local'):
        path = BINARY.parent / name
        if not path.exists() and not path.is_symlink():
            configs[name] = None
            continue
        file_metadata = metadata(path)
        contents = path.read_bytes()
        configs[name] = dict(metadata=file_metadata, sha=digest(contents))
        snapshots[name + '.previous'] = contents
    backup.mkdir(mode=0o700)
    os.chmod(backup, 0o700)
    for name, contents in snapshots.items():
        atomic_write(backup / name, contents)
    state = dict(status='prepared', binary=str(BINARY), old_sha=expected_sha, candidate_sha=candidate_sha, binary_metadata=binary_meta,
                 original_process=current, active_process=None, configs=configs, hashes={name: digest(value) for name, value in snapshots.items()})
    verify_binary(expected_sha)
    verify_configs(backup, state)
    verify_runtime(backup, current, expected_sha)
    save_state(backup, state)
    return state


def stop_process(expected):
    try:
        actual = identity(expected['pid'])
    except (FileNotFoundError, ProcessLookupError):
        return
    if actual != expected:
        raise RuntimeError('refusing to signal a replaced process')
    fd = os.pidfd_open(expected['pid'])
    try:
        if identity(expected['pid']) != expected:
            raise RuntimeError('process changed before signal')
        signal.pidfd_send_signal(fd, signal.SIGTERM)
        if not select.select([fd], [], [], 15)[0]:
            raise RuntimeError('service did not stop; no force kill attempted')
    finally:
        os.close(fd)


def install(backup, state, name):
    verify_configs(backup, state)
    if find_processes():
        raise RuntimeError('cannot replace binary while a service process is running')
    regular(BINARY)
    if digest(BINARY.read_bytes()) not in (state['old_sha'], state['candidate_sha']):
        raise RuntimeError('unreviewed binary appeared; refusing overwrite')
    contents = (backup / name).read_bytes()
    if digest(contents) != state['hashes'][name]:
        raise RuntimeError('binary snapshot changed before installation')
    atomic_write(BINARY, contents, **state['binary_metadata'])


def start_process(backup, state, sha):
    verify_configs(backup, state)
    verify_binary(sha)
    if find_processes():
        raise RuntimeError('refusing duplicate service process')
    environment = json.loads((backup / 'runtime-env.json').read_text())
    arguments = json.loads((backup / 'cmdline.json').read_text())
    fd = os.open(backup / 'process.log', os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'ab') as output:
        os.fchmod(output.fileno(), 0o600)
        process = subprocess.Popen(arguments, executable=str(BINARY), cwd=state['original_process']['cwd'], env=environment,
                                   stdin=subprocess.DEVNULL, stdout=output, stderr=output, start_new_session=True, close_fds=True)
    owned_fd = os.pidfd_open(process.pid)
    try:
        for _ in range(20):
            if process.poll() is not None:
                raise RuntimeError('service exited immediately; inspect private process log')
            try:
                current = identity(process.pid)
                if current['exe'] == str(BINARY):
                    state['active_process'] = current
                    save_state(backup, state)
                    verify_runtime(backup, current, sha)
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


def health_status(path):
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open('http://127.0.0.1:8080' + path, timeout=2) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except (OSError, urllib.error.URLError):
        return None


def wait_healthy(expected):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if find_processes() != [expected]:
            raise RuntimeError('started process disappeared or was replaced')
        if health_status('/api/v1/miniprogram/tracking/config') == 200 and health_status('/api/v1/miniprogram/workbench/context') == 401:
            return
        time.sleep(0.5)
    raise RuntimeError('service health check failed')


def restore(backup, state):
    verify_configs(backup, state)
    current = find_processes()
    if current:
        expected = state.get('active_process') or state['original_process']
        if current != [expected]:
            raise RuntimeError('unknown service process blocks rollback')
        # The original may still be running if SIGTERM failed. Preserve healthy service.
        if expected == state['original_process']:
            verify_binary(state['old_sha'])
            verify_runtime(backup, expected, state['old_sha'])
            wait_healthy(expected)
            state.update(status='rolled_back', active_process=expected)
            save_state(backup, state)
            return state
        stop_process(expected)
    state.update(status='restoring', active_process=None)
    save_state(backup, state)
    install(backup, state, 'binary.previous')
    restored = start_process(backup, state, state['old_sha'])
    wait_healthy(restored)
    state['status'] = 'rolled_back'
    save_state(backup, state)
    return state


def activate(backup):
    state = load_state(backup)
    if state['status'] != 'prepared':
        raise RuntimeError('activation requires prepared state')
    verify_binary(state['old_sha'])
    if metadata(BINARY) != state['binary_metadata']:
        raise RuntimeError('binary ownership or permissions changed after prepare')
    verify_configs(backup, state)
    verify_runtime(backup, state['original_process'], state['old_sha'])
    try:
        state['status'] = 'stopping'
        save_state(backup, state)
        stop_process(state['original_process'])
        state['status'] = 'installing'
        save_state(backup, state)
        install(backup, state, 'binary.candidate')
        state['status'] = 'starting'
        save_state(backup, state)
        started = start_process(backup, state, state['candidate_sha'])
        wait_healthy(started)
        verify_configs(backup, state)
        verify_runtime(backup, started, state['candidate_sha'])
        state['status'] = 'active'
        save_state(backup, state)
        return state
    except BaseException:
        restore(backup, state)
        raise RuntimeError('activation failed; original binary and runtime restored') from None


def rollback(backup):
    state = load_state(backup)
    if state['status'] not in ('stopping', 'installing', 'starting', 'active', 'restoring', 'rolled_back'):
        raise RuntimeError('rollback requires an attempted activation')
    if state['status'] == 'rolled_back':
        verify_configs(backup, state)
        verify_binary(state['old_sha'])
        verify_runtime(backup, state['active_process'], state['old_sha'])
        wait_healthy(state['active_process'])
        return state
    return restore(backup, state)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('prepare', 'activate', 'rollback'))
    parser.add_argument('--backup', required=True, type=Path)
    parser.add_argument('--candidate', type=Path)
    parser.add_argument('--candidate-sha')
    parser.add_argument('--expected-pid', type=int, default=EXPECTED_PID)
    parser.add_argument('--expected-old-sha', default=EXPECTED_SHA)
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if os.geteuid() != 0:
            raise RuntimeError('root operator required')
        # One lock for the deployment, including separate release backup directories.
        lock = os.open(BINARY.parent / '.binary-release.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if args.action == 'prepare':
                if not args.candidate or not args.candidate_sha:
                    raise RuntimeError('candidate and its reviewed SHA are required')
                state = prepare(args.backup, args.candidate, args.candidate_sha, args.expected_pid, args.expected_old_sha)
            else:
                state = {'activate': activate, 'rollback': rollback}[args.action](args.backup)
        finally:
            os.close(lock)
    except BaseException as error:
        try:
            private_directory(args.backup)
            fd = os.open(args.backup / 'errors.log', os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'a') as output:
                os.fchmod(output.fileno(), 0o600)
                traceback.print_exc(file=output)
        except (OSError, RuntimeError):
            pass
        print('FAILED: ' + type(error).__name__ + '; inspect private backup state and logs')
        return 1
    summary = {key: state[key] for key in ('status', 'old_sha', 'candidate_sha')}
    summary['pid'] = (state.get('active_process') or state['original_process'])['pid']
    print(json.dumps(summary))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
