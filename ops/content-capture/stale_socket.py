#!/usr/bin/env python3
"""Remove only a dead, worker-owned capture socket before service startup."""
import argparse
import errno
import os
import pathlib
import pwd
import socket
import stat


SOCKET_PATH = pathlib.Path('/run/jdlg-content-capture/capture.sock')


def remove_stale(path, expected_uid):
    path = pathlib.Path(path)
    try:
        before = path.lstat()
    except FileNotFoundError:
        return
    if stat.S_ISLNK(before.st_mode):
        raise RuntimeError('capture socket is a symlink')
    if not stat.S_ISSOCK(before.st_mode):
        raise RuntimeError('capture socket path is not a socket')
    if before.st_uid != expected_uid:
        raise RuntimeError('capture socket owner does not match the worker')

    client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    client.settimeout(0.5)
    try:
        client.connect(str(path))
    except OSError as error:
        if error.errno != errno.ECONNREFUSED:
            raise RuntimeError('capture socket state cannot be proven stale') from error
    else:
        raise RuntimeError('capture socket is active')
    finally:
        client.close()

    after = path.lstat()
    if (after.st_dev, after.st_ino, after.st_uid, after.st_mode) != (
        before.st_dev, before.st_ino, before.st_uid, before.st_mode
    ):
        raise RuntimeError('capture socket changed during validation')
    path.unlink()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--user', required=True)
    arguments = parser.parse_args()
    remove_stale(SOCKET_PATH, pwd.getpwnam(arguments.user).pw_uid)


if __name__ == '__main__':
    main()
