#!/usr/bin/env python3
"""Own only the dedicated capture namespace and its named firewall chains."""
import ipaddress
import json
import os
import pathlib
import socket
import subprocess
import sys

NS = 'jdlg-content-capture'
HOST = 'jdcap-host'
PEER = 'jdcap-ns'
HOST_IP = '192.0.2.249'
BROWSER_IP = '192.0.2.250'
LINK = '192.0.2.248/30'
DNS = '119.29.29.29'
ROOT = pathlib.Path('/opt/jdlg-content-capture')
STATE = pathlib.Path('/run/jdlg-content-capture-network-owned.json')
HOSTS = ('mp.weixin.qq.com', 'res.wx.qq.com', 'mpvideo.qpic.cn',
         'mmbiz.qpic.cn', 'mmbiz.qlogo.cn', 'wxsnsdy.wxs.qq.com')


def public_ipv4(raw):
    try:
        ip = ipaddress.ip_address(raw)
        return ip.version == 4 and ip.is_global and not ip.is_multicast
    except ValueError:
        return False


def checked_ips(ips):
    values = sorted(set(ips))
    if not values or len(values) > 256 or any(not public_ipv4(ip) for ip in values):
        raise ValueError('approved public IPv4 addresses required')
    return values


def namespace_rules(ips):
    ips = checked_ips(ips)
    lines = ['*filter', ':INPUT DROP [0:0]', ':FORWARD DROP [0:0]',
             ':OUTPUT DROP [0:0]', ':JDCAP_OUT - [0:0]', '-F JDCAP_OUT',
             '-F INPUT', '-F OUTPUT', '-F FORWARD', '-A INPUT -i lo -j ACCEPT',
             '-A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT',
             '-A OUTPUT -j JDCAP_OUT', '-A JDCAP_OUT -o lo -j ACCEPT']
    for protocol in ('udp', 'tcp'):
        lines.append(f'-A JDCAP_OUT -d {DNS} -p {protocol} --dport 53 -j ACCEPT')
    for ip in ips:
        lines.append(f'-A JDCAP_OUT -d {ip} -p tcp --dport 443 -j ACCEPT')
    return '\n'.join(lines + ['-A JDCAP_OUT -j DROP', 'COMMIT', ''])


def host_rules(ips):
    ips = checked_ips(ips)
    lines = ['*filter', ':JDCAP_FORWARD - [0:0]', ':JDCAP_HOST_IN - [0:0]',
             '-F JDCAP_FORWARD', '-F JDCAP_HOST_IN', '-A JDCAP_HOST_IN -j DROP']
    for protocol in ('udp', 'tcp'):
        lines.append(f'-A JDCAP_FORWARD -i {HOST} -s {BROWSER_IP}/32 -d {DNS} -p {protocol} --dport 53 -j ACCEPT')
    for ip in ips:
        lines.append(f'-A JDCAP_FORWARD -i {HOST} -s {BROWSER_IP}/32 -d {ip} -p tcp --dport 443 -j ACCEPT')
    lines += [f'-A JDCAP_FORWARD -i {HOST} -j DROP',
              f'-A JDCAP_FORWARD -o {HOST} -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT',
              '-A JDCAP_FORWARD -j DROP', 'COMMIT', '']
    return '\n'.join(lines)


def run(args, *, data=None, check=True):
    result = subprocess.run(args, input=data, text=True, capture_output=True, timeout=30)
    if check and result.returncode:
        raise RuntimeError('command failed: ' + ' '.join(args[:4]) + ': ' + result.stderr[:300])
    return result


def inside(args, **kwargs):
    return run(['ip', 'netns', 'exec', NS] + args, **kwargs)


def resolved_ips():
    values = set()
    for host in HOSTS:
        answers = socket.getaddrinfo(host, 443, socket.AF_INET, socket.SOCK_STREAM)
        addresses = {answer[4][0] for answer in answers}
        if not addresses or any(not public_ipv4(ip) for ip in addresses):
            raise RuntimeError('domain has no approved public IPv4: ' + host)
        values.update(addresses)
    return checked_ips(values)


def ensure_jump(chain, rule):
    if run(['iptables', '-w', '-C', chain] + rule, check=False).returncode:
        run(['iptables', '-w', '-I', chain, '1'] + rule)


def owned():
    if not STATE.is_file() or STATE.is_symlink():
        raise RuntimeError('namespace ownership record missing')
    state = json.loads(STATE.read_text())
    path = pathlib.Path('/run/netns') / NS
    if path.exists() and path.stat().st_ino != state['inode']:
        raise RuntimeError('namespace ownership mismatch')
    return state


def refresh():
    owned()
    ips = resolved_ips()
    # Each table transaction is atomic. An error retains the previous deny policy.
    inside(['iptables-restore', '-w', '--noflush'], data=namespace_rules(ips))
    run(['iptables-restore', '-w', '--noflush'], data=host_rules(ips))
    (ROOT / 'network-status.json').write_text(json.dumps({'hosts': HOSTS, 'approved_ipv4': ips}))
    print('CAPTURE_NETWORK_READY approved_ipv4=' + str(len(ips)), flush=True)


def up():
    if STATE.exists():
        if not owned().get('complete'):
            raise RuntimeError('incomplete previous setup; run owned rollback first')
        refresh()
        return
    if pathlib.Path('/run/netns', NS).exists() or run(['ip', 'link', 'show', HOST], check=False).returncode == 0:
        raise RuntimeError('refusing pre-existing namespace/interface')
    if pathlib.Path('/proc/sys/net/ipv4/ip_forward').read_text().strip() != '1':
        raise RuntimeError('host forwarding is disabled; no global sysctl changes applied')
    routes = json.loads(run(['ip', '-j', '-4', 'route', 'show', 'table', 'all']).stdout)
    link = ipaddress.ip_network(LINK)
    for route in routes:
        destination = route.get('dst', 'default')
        if destination != 'default' and link.overlaps(ipaddress.ip_network(destination, strict=False)):
            raise RuntimeError('dedicated link conflicts with an existing route')
    for chain in ('JDCAP_FORWARD', 'JDCAP_HOST_IN'):
        if run(['iptables', '-w', '-S', chain], check=False).returncode == 0:
            raise RuntimeError('refusing pre-existing firewall chain')
    if run(['iptables', '-w', '-t', 'nat', '-S', 'JDCAP_NAT'], check=False).returncode == 0:
        raise RuntimeError('refusing pre-existing NAT chain')
    ips = resolved_ips()
    run(['ip', 'netns', 'add', NS])
    STATE.write_text(json.dumps({'namespace': NS, 'inode': pathlib.Path('/run/netns', NS).stat().st_ino}))
    STATE.chmod(0o600)
    # No interface is usable before both deny policies are installed.
    inside(['sysctl', '-q', '-w', 'net.ipv6.conf.all.disable_ipv6=1', 'net.ipv6.conf.default.disable_ipv6=1'])
    inside(['iptables-restore', '-w', '--noflush'], data=namespace_rules(ips))
    run(['iptables-restore', '-w', '--noflush'], data=host_rules(ips))
    ensure_jump('INPUT', ['-i', HOST, '-j', 'JDCAP_HOST_IN'])
    ensure_jump('FORWARD', ['-i', HOST, '-j', 'JDCAP_FORWARD'])
    ensure_jump('FORWARD', ['-o', HOST, '-j', 'JDCAP_FORWARD'])
    run(['ip', 'link', 'add', HOST, 'type', 'veth', 'peer', 'name', PEER])
    run(['ip', 'link', 'set', PEER, 'netns', NS])
    run(['ip', 'addr', 'add', HOST_IP + '/30', 'dev', HOST])
    inside(['ip', 'addr', 'add', BROWSER_IP + '/30', 'dev', PEER])
    run(['iptables', '-w', '-t', 'nat', '-N', 'JDCAP_NAT'])
    run(['iptables', '-w', '-t', 'nat', '-A', 'JDCAP_NAT', '-s', BROWSER_IP + '/32', '-j', 'MASQUERADE'])
    run(['iptables', '-w', '-t', 'nat', '-I', 'POSTROUTING', '1', '-s', BROWSER_IP + '/32', '-j', 'JDCAP_NAT'])
    run(['ip', 'link', 'set', HOST, 'up'])
    inside(['ip', 'link', 'set', 'lo', 'up'])
    inside(['ip', 'link', 'set', PEER, 'up'])
    inside(['ip', 'route', 'add', 'default', 'via', HOST_IP])
    STATE.write_text(json.dumps({'namespace': NS, 'inode': pathlib.Path('/run/netns', NS).stat().st_ino, 'complete': True}))
    (ROOT / 'network-status.json').write_text(json.dumps({'hosts': HOSTS, 'approved_ipv4': ips}))
    print('CAPTURE_NETWORK_READY approved_ipv4=' + str(len(ips)), flush=True)


def down():
    owned()
    for chain, rule in [('INPUT', ['-i', HOST, '-j', 'JDCAP_HOST_IN']),
                        ('FORWARD', ['-i', HOST, '-j', 'JDCAP_FORWARD']),
                        ('FORWARD', ['-o', HOST, '-j', 'JDCAP_FORWARD'])]:
        while run(['iptables', '-w', '-C', chain] + rule, check=False).returncode == 0:
            run(['iptables', '-w', '-D', chain] + rule)
    for chain in ('JDCAP_FORWARD', 'JDCAP_HOST_IN'):
        run(['iptables', '-w', '-F', chain], check=False)
        run(['iptables', '-w', '-X', chain], check=False)
    rule = ['-s', BROWSER_IP + '/32', '-j', 'JDCAP_NAT']
    while run(['iptables', '-w', '-t', 'nat', '-C', 'POSTROUTING'] + rule, check=False).returncode == 0:
        run(['iptables', '-w', '-t', 'nat', '-D', 'POSTROUTING'] + rule)
    run(['iptables', '-w', '-t', 'nat', '-F', 'JDCAP_NAT'], check=False)
    run(['iptables', '-w', '-t', 'nat', '-X', 'JDCAP_NAT'], check=False)
    run(['ip', 'link', 'del', HOST], check=False)
    if pathlib.Path('/run/netns', NS).exists():
        run(['ip', 'netns', 'del', NS])
    STATE.unlink()
    print('CAPTURE_NETWORK_REMOVED', flush=True)


if __name__ == '__main__':
    if os.geteuid() != 0 or len(sys.argv) != 2 or sys.argv[1] not in ('up', 'refresh', 'down'):
        raise SystemExit('root and explicit up/refresh/down action required')
    try:
        {'up': up, 'refresh': refresh, 'down': down}[sys.argv[1]]()
    except Exception as error:
        print('CAPTURE_NETWORK_FAILED ' + str(error), file=sys.stderr, flush=True)
        raise SystemExit(1)
