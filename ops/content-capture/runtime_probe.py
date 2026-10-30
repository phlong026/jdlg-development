#!/usr/bin/env python3
"""Read-only acceptance probe for the dedicated content capture runtime."""
import argparse
import json
import os
import pathlib
import pwd
import socket
import subprocess
import sys


NAMESPACE = 'jdlg-content-capture'
NAMESPACE_PATH = pathlib.Path('/run/netns/jdlg-content-capture')
DEPLOYED_SCRIPT = '/opt/jdlg-content-capture/ops/runtime_probe.py'
BROWSER_UNIT = 'jdlg-content-capture-browser.service'
WORKER_UNIT = 'jdlg-content-capture-worker.service'
SYSTEMCTL = '/usr/bin/systemctl'
IP = '/usr/sbin/ip'
RUNUSER = '/usr/sbin/runuser'
PYTHON = '/usr/bin/python3'
CURL = '/usr/bin/curl'
SOCKET_TIMEOUT = 1.0
COMMAND_TIMEOUT = 3
INSIDE_PROCESS_TIMEOUT = 25

BLOCKED_TARGETS = (
    ('metadata_blocked', '169.254.169.254', 80),
    ('private_blocked', '10.0.0.1', 80),
    ('vpc_blocked', '172.30.0.1', 80),
    ('host_veth_blocked', '192.0.2.249', 8080),
    ('unlisted_public_blocked', '8.8.8.8', 443),
    ('other_dns_blocked', '1.1.1.1', 53),
    ('ipv6_loopback_blocked', '::1', 443),
)


def parse_systemd_show(raw):
    values = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
    try:
        pid = int(values.get('MainPID', '0'))
    except ValueError:
        pid = 0
    return {'active': values.get('ActiveState') == 'active', 'pid': pid}


def parse_proc_status(raw):
    values = {}
    for line in raw.splitlines():
        if ':' in line:
            key, value = line.split(':', 1)
            values[key] = value.strip()
    return {
        'uid': int(values['Uid'].split()[0]),
        'cap_eff': int(values['CapEff'], 16),
        'no_new_privs': int(values['NoNewPrivs']),
    }


def socket_blocked(host, port, connector=socket.create_connection):
    try:
        connection = connector((host, port), timeout=SOCKET_TIMEOUT)
    except OSError:
        return True
    connection.close()
    return False


def finalize(checks):
    normalized = {name: bool(value) for name, value in checks.items()}
    return {'ok': bool(normalized) and all(normalized.values()), 'checks': normalized}


def inside_command():
    return [
        IP, 'netns', 'exec', NAMESPACE,
        RUNUSER, '-u', 'jdlgcap-browser', '--',
        PYTHON, DEPLOYED_SCRIPT, '--inside-network',
    ]


def unit_state(unit):
    completed = subprocess.run(
        [SYSTEMCTL, 'show', unit, '--property=ActiveState', '--property=MainPID'],
        check=True,
        capture_output=True,
        text=True,
        timeout=COMMAND_TIMEOUT,
    )
    return parse_systemd_show(completed.stdout)


def process_status(pid):
    return parse_proc_status(pathlib.Path('/proc', str(pid), 'status').read_text())


def process_net_inode(pid):
    return pathlib.Path('/proc', str(pid), 'ns', 'net').stat().st_ino


def wwwroot_hidden(pid):
    target = pathlib.Path('/proc', str(pid), 'root', 'www', 'wwwroot')
    try:
        target.stat()
    except (FileNotFoundError, PermissionError):
        return True
    return False


def trusted_https_available():
    completed = subprocess.run(
        [
            CURL, '--noproxy', '*', '--max-time', '10', '--connect-timeout', '5',
            '--silent', '--show-error', '--output', '/dev/null',
            '--write-out', '%{http_code}', '--proto', '=https',
            'https://mp.weixin.qq.com/',
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=12,
    )
    code = completed.stdout.strip()
    return completed.returncode == 0 and code.isdigit() and int(code) > 0


def probe_inside_network():
    expected_uid = pwd.getpwnam('jdlgcap-browser').pw_uid
    if os.geteuid() != expected_uid or expected_uid == 0:
        return finalize({'browser_user': False})
    checks = {'browser_user': True}
    for name, host, port in BLOCKED_TARGETS:
        checks[name] = socket_blocked(host, port)
    checks['ipv6_disabled'] = all(
        pathlib.Path('/proc/sys/net/ipv6/conf', scope, 'disable_ipv6').read_text().strip() == '1'
        for scope in ('all', 'default')
    )
    checks['trusted_https'] = trusted_https_available()
    return finalize(checks)


def probe_host():
    browser = unit_state(BROWSER_UNIT)
    worker = unit_state(WORKER_UNIT)
    checks = {
        'root_entry': os.geteuid() == 0,
        'browser_active': browser['active'] and browser['pid'] > 0,
        'worker_active': worker['active'] and worker['pid'] > 0,
        'host_wwwroot_present': pathlib.Path('/www/wwwroot').is_dir(),
        'host_cdp_blocked': socket_blocked('127.0.0.1', 9222),
    }
    if browser['pid'] > 0 and worker['pid'] > 0:
        browser_status = process_status(browser['pid'])
        worker_status = process_status(worker['pid'])
        namespace_inode = NAMESPACE_PATH.stat().st_ino
        host_inode = pathlib.Path('/proc/self/ns/net').stat().st_ino
        browser_inode = process_net_inode(browser['pid'])
        worker_inode = process_net_inode(worker['pid'])
        checks.update({
            'shared_isolated_netns': (
                browser_inode == namespace_inode
                and worker_inode == namespace_inode
                and namespace_inode != host_inode
            ),
            'dedicated_nonroot_users': (
                browser_status['uid'] != 0
                and worker_status['uid'] != 0
                and browser_status['uid'] != worker_status['uid']
            ),
            'browser_zero_capabilities': browser_status['cap_eff'] == 0,
            'worker_zero_capabilities': worker_status['cap_eff'] == 0,
            'browser_no_new_privileges': browser_status['no_new_privs'] == 1,
            'worker_no_new_privileges': worker_status['no_new_privs'] == 1,
            'browser_wwwroot_hidden': wwwroot_hidden(browser['pid']),
            'worker_wwwroot_hidden': wwwroot_hidden(worker['pid']),
        })
    else:
        checks['process_security'] = False

    completed = subprocess.run(
        inside_command(),
        check=False,
        capture_output=True,
        text=True,
        timeout=INSIDE_PROCESS_TIMEOUT,
    )
    try:
        inside = json.loads(completed.stdout)
    except (json.JSONDecodeError, TypeError):
        inside = {'ok': False, 'checks': {'inside_output_valid': False}}
    checks['inside_network_probe'] = completed.returncode == 0 and inside.get('ok') is True
    result = finalize(checks)
    result['inside'] = inside
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--inside-network', action='store_true')
    arguments = parser.parse_args()
    if not arguments.inside_network and os.geteuid() != 0:
        result = finalize({'root_entry': False})
        print(json.dumps(result, sort_keys=True, separators=(',', ':')))
        return 1
    try:
        result = probe_inside_network() if arguments.inside_network else probe_host()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        result = finalize({'probe_completed': False})
    print(json.dumps(result, sort_keys=True, separators=(',', ':')))
    return 0 if result['ok'] else 1


if __name__ == '__main__':
    sys.exit(main())
