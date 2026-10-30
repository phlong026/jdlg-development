import importlib.util
import pathlib
import unittest


class NetworkPolicyTest(unittest.TestCase):
    def load(self):
        path = pathlib.Path(__file__).with_name('network.py')
        spec = importlib.util.spec_from_file_location('capture_network', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_destinations_reject_private_metadata_reserved_and_ipv6(self):
        module = self.load()
        for raw in ['127.0.0.1', '10.0.0.1', '169.254.169.254', '100.64.0.1', '192.0.2.1', '224.0.0.1', '::1', '2606:4700::1111']:
            self.assertFalse(module.public_ipv4(raw), raw)
        self.assertTrue(module.public_ipv4('119.29.29.29'))

    def test_policy_has_only_approved_https_and_fixed_dns(self):
        module = self.load()
        rules = module.namespace_rules(['119.29.29.29', '1.2.3.4'])
        self.assertIn(':OUTPUT DROP', rules)
        self.assertIn('-d 1.2.3.4 -p tcp --dport 443 -j ACCEPT', rules)
        self.assertIn('-d 119.29.29.29 -p udp --dport 53 -j ACCEPT', rules)
        self.assertNotIn('--dport 80', rules)
        self.assertNotIn('0.0.0.0/0', rules)
        self.assertIn('-A JDCAP_OUT -j DROP', rules)
        with self.assertRaises(ValueError):
            module.namespace_rules(['169.254.169.254'])

    def test_host_policy_drops_access_to_host_and_unapproved_forwarding(self):
        module = self.load()
        rules = module.host_rules(['1.2.3.4'])
        self.assertIn('-A JDCAP_HOST_IN -j DROP', rules)
        self.assertIn('-A JDCAP_FORWARD -i jdcap-host -j DROP', rules)
        self.assertIn('--ctstate ESTABLISHED,RELATED', rules)
        self.assertIn('-A JDCAP_FORWARD -j DROP', rules)
        with self.assertRaises(ValueError):
            module.host_rules([])


if __name__ == '__main__':
    unittest.main()
