#!/usr/bin/env python3
"""Run the existing private COS live test on its credential-owning server."""
import hashlib
import os
from pathlib import Path
import subprocess


def main():
    binary = Path('/root/jdlg-private-cos-test')
    expected = '4b0119af576b067db5400627c77dc4ee72b5971ae630274daccd31b3984c3e72'
    if hashlib.sha256(binary.read_bytes()).hexdigest() != expected:
        raise RuntimeError('private COS test checksum mismatch')
    pids = subprocess.check_output(['pgrep', '-x', 'applinux'], timeout=5).decode().split()
    if len(pids) != 1:
        raise RuntimeError('expected exactly one current application')
    proc = Path('/proc') / pids[0]
    if os.readlink(proc / 'exe') != '/www/wwwroot/jiadilingguang/service/bin/applinux':
        raise RuntimeError('unexpected application location')
    env = dict(item.split('=', 1) for item in (proc / 'environ').read_bytes().decode().split('\0') if '=' in item)
    env.update(JDLG_PHASE3_COS_LIVE='true', CONTENT_MEDIA_COS_PREFIX='jd-content-private')
    # Match service/config/config.go when production relies on its existing defaults.
    if not env.get('COS_BUCKET'):
        env['COS_BUCKET'] = 'jiadilingguangcos-1393500756'
    if not env.get('COS_REGION'):
        env['COS_REGION'] = 'ap-chongqing'
    binary.chmod(0o700)
    log = Path('/root/jdlg-private-cos-verification.log')
    with log.open('wb') as output:
        os.chmod(log, 0o600)
        result = subprocess.run([str(binary), '-test.run', '^TestLivePrivateCOSRoundTrip$', '-test.v'],
                                env=env, stdout=output, stderr=output, timeout=180)
    text = log.read_text()
    if result.returncode != 0 or 'LIVE_COS_OK' not in text:
        raise RuntimeError('private COS check failed; inspect private server log')
    print('PRIVATE_COS_OK private_acl=true anonymous_denied=true sha256_verified=true overwrite_rejected=true range_verified=true owned_test_object_cleaned=true')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
