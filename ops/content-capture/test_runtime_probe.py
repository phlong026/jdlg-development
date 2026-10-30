import importlib.util
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parent


class RuntimeProbeTest(unittest.TestCase):
    def load(self):
        path = ROOT / 'runtime_probe.py'
        spec = importlib.util.spec_from_file_location('runtime_probe', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_parses_only_required_systemd_and_proc_fields(self):
        module = self.load()
        self.assertEqual(
            module.parse_systemd_show('ActiveState=active\nMainPID=321\n'),
            {'active': True, 'pid': 321},
        )
        self.assertEqual(
            module.parse_proc_status('Name:\ttest\nUid:\t1001\t1001\t1001\t1001\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\n'),
            {'uid': 1001, 'cap_eff': 0, 'no_new_privs': 1},
        )

    def test_blocked_socket_probe_uses_one_second_timeout_without_network(self):
        module = self.load()
        calls = []

        def blocked(address, timeout):
            calls.append((address, timeout))
            raise TimeoutError('blocked')

        self.assertTrue(module.socket_blocked('10.0.0.1', 80, connector=blocked))
        self.assertEqual(calls, [(('10.0.0.1', 80), 1.0)])

        class Connection:
            closed = False

            def close(self):
                self.closed = True

        connection = Connection()
        self.assertFalse(module.socket_blocked('8.8.8.8', 443, connector=lambda *_args, **_kwargs: connection))
        self.assertTrue(connection.closed)

    def test_inside_command_is_fixed_nonroot_and_bounded(self):
        module = self.load()
        command = module.inside_command()
        self.assertEqual(command[:6], [
            '/usr/sbin/ip', 'netns', 'exec', 'jdlg-content-capture',
            '/usr/sbin/runuser', '-u',
        ])
        self.assertEqual(command[6:9], ['jdlgcap-browser', '--', '/usr/bin/python3'])
        self.assertEqual(command[-2:], ['/opt/jdlg-content-capture/ops/runtime_probe.py', '--inside-network'])
        self.assertLessEqual(module.INSIDE_PROCESS_TIMEOUT, 25)

    def test_inside_result_requires_every_negative_probe_and_trusted_https(self):
        module = self.load()
        checks = {name: True for name, _host, _port in module.BLOCKED_TARGETS}
        checks['trusted_https'] = True
        self.assertTrue(module.finalize(checks)['ok'])
        checks['metadata_blocked'] = False
        self.assertFalse(module.finalize(checks)['ok'])

    def test_source_never_reads_secrets_or_mutates_services(self):
        source = (ROOT / 'runtime_probe.py').read_text()
        for forbidden in ('cmdline', 'environ', 'CONTENT_CAPTURE_SECRET', 'sudo',
                          'systemctl start', 'systemctl stop', 'systemctl restart'):
            self.assertNotIn(forbidden, source)
        self.assertIn("('/www/wwwroot').is_dir()", source)
        self.assertIn("'CapEff'", source)
        self.assertIn("'NoNewPrivs'", source)


if __name__ == '__main__':
    unittest.main()
