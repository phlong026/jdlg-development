import importlib.util
import os
import pathlib
import socket
import tempfile
import unittest


class StaleSocketTest(unittest.TestCase):
    def load(self):
        path = pathlib.Path(__file__).with_name('stale_socket.py')
        spec = importlib.util.spec_from_file_location('stale_socket', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def runtime(self):
        temporary = tempfile.TemporaryDirectory(dir='/tmp')
        self.addCleanup(temporary.cleanup)
        directory = pathlib.Path(temporary.name) / 'jdlg-content-capture'
        directory.mkdir(mode=0o750)
        return directory

    def test_missing_socket_is_a_noop(self):
        module = self.load()
        module.remove_stale(self.runtime() / 'capture.sock', os.geteuid())

    def test_closed_owned_socket_is_removed(self):
        module = self.load()
        path = self.runtime() / 'capture.sock'
        listener = socket.socket(socket.AF_UNIX)
        listener.bind(str(path))
        listener.close()
        module.remove_stale(path, os.geteuid())
        self.assertFalse(path.exists())

    def test_active_socket_is_never_removed(self):
        module = self.load()
        path = self.runtime() / 'capture.sock'
        listener = socket.socket(socket.AF_UNIX)
        listener.bind(str(path))
        listener.listen(1)
        self.addCleanup(listener.close)
        with self.assertRaisesRegex(RuntimeError, 'active'):
            module.remove_stale(path, os.geteuid())
        self.assertTrue(path.exists())

    def test_symlink_and_wrong_owner_are_rejected(self):
        module = self.load()
        directory = self.runtime()
        target = directory / 'target'
        target.write_text('not a socket')
        linked = directory / 'capture.sock'
        linked.symlink_to(target)
        with self.assertRaisesRegex(RuntimeError, 'symlink'):
            module.remove_stale(linked, os.geteuid())
        linked.unlink()

        listener = socket.socket(socket.AF_UNIX)
        listener.bind(str(linked))
        listener.close()
        with self.assertRaisesRegex(RuntimeError, 'owner'):
            module.remove_stale(linked, os.geteuid() + 1)


if __name__ == '__main__':
    unittest.main()
