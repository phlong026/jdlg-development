import pathlib
import re
import unittest


ROOT = pathlib.Path(__file__).resolve().parent


class DeploymentPolicyTest(unittest.TestCase):
    def read(self, relative):
        return (ROOT / relative).read_text()

    def test_units_share_the_dedicated_namespace_and_harden_processes(self):
        for name in ('jdlg-content-capture-browser.service', 'jdlg-content-capture-worker.service'):
            unit = self.read('systemd/' + name)
            self.assertIn('NetworkNamespacePath=/run/netns/jdlg-content-capture', unit)
            self.assertIn('BindReadOnlyPaths=/etc/netns/jdlg-content-capture/resolv.conf:/etc/resolv.conf', unit)
            self.assertIn('ProtectSystem=strict', unit)
            self.assertIn('ProtectHome=true', unit)
            self.assertIn('NoNewPrivileges=true', unit)
            self.assertIn('InaccessiblePaths=/www', unit)
            self.assertIn('ProtectProc=invisible', unit)
            self.assertRegex(unit, r'(?m)^MemoryMax=\S+')
            self.assertRegex(unit, r'(?m)^TasksMax=\d+')

    def test_network_lifecycle_runs_in_the_host_mount_namespace_with_narrow_capabilities(self):
        unit = self.read('systemd/jdlg-content-capture-network.service')
        for forbidden in ('ProtectSystem=', 'ProtectHome=', 'PrivateTmp=', 'ReadWritePaths=',
                          'InaccessiblePaths=', 'ProtectProc='):
            self.assertNotIn(forbidden, unit)
        self.assertIn('CapabilityBoundingSet=CAP_NET_ADMIN CAP_SYS_ADMIN', unit)
        self.assertIn('NoNewPrivileges=true', unit)
        browser = self.read('systemd/jdlg-content-capture-browser.service')
        worker = self.read('systemd/jdlg-content-capture-worker.service')
        self.assertNotIn('CAP_SYS_ADMIN', browser)
        self.assertNotIn('CAP_SYS_ADMIN', worker)

    def test_browser_exposes_cdp_only_on_loopback_without_sandbox_bypasses(self):
        unit = self.read('systemd/jdlg-content-capture-browser.service')
        self.assertIn('--remote-debugging-address=127.0.0.1', unit)
        self.assertIn('--remote-debugging-port=9222', unit)
        self.assertIn('--remote-allow-origins=http://localhost/', unit)
        self.assertIn('--user-data-dir=/run/jdlg-content-capture-browser/profile', unit)
        for forbidden in ('--no-sandbox', '--disable-setuid-sandbox', 'SYS_ADMIN', 'seccomp=unconfined'):
            self.assertNotIn(forbidden, unit)
        self.assertIn('156.0.8078.47', self.read('deploy.sh'))

    def test_worker_uses_dedicated_socket_and_verified_stale_socket_cleanup(self):
        unit = self.read('systemd/jdlg-content-capture-worker.service')
        self.assertIn('RuntimeDirectory=jdlg-content-capture', unit)
        self.assertIn('RuntimeDirectoryMode=0750', unit)
        self.assertIn('User=jdlgcap-worker', unit)
        self.assertIn('Group=www', unit)
        self.assertIn('stale_socket.py', unit)
        example = self.read('worker.env.example')
        self.assertIn('CONTENT_CAPTURE_LISTEN=unix:/run/jdlg-content-capture/capture.sock', example)
        self.assertIn('CONTENT_CAPTURE_NETWORK_ISOLATED=true', example)
        self.assertIn('CONTENT_CAPTURE_CDP_DISCOVERY_URL=http://127.0.0.1:9222/json/version', example)
        self.assertNotIn('CONTENT_CAPTURE_SECRET=', example)

    def test_browser_restart_does_not_remove_worker_health_endpoint(self):
        unit = self.read('systemd/jdlg-content-capture-worker.service')
        self.assertIn('Requires=jdlg-content-capture-network.service\n', unit)
        self.assertIn('Wants=jdlg-content-capture-browser.service\n', unit)
        self.assertNotIn('Requires=jdlg-content-capture-network.service jdlg-content-capture-browser.service', unit)

    def test_nginx_locations_are_loopback_only_and_do_not_log_requests(self):
        nginx = self.read('nginx/content-capture.conf')
        for path in ('/_internal/content-capture/v1/capture', '/_internal/content-capture/ready'):
            self.assertIn('location = ' + path, nginx)
        self.assertGreaterEqual(nginx.count('allow 127.0.0.1;'), 2)
        self.assertGreaterEqual(nginx.count('allow ::1;'), 2)
        self.assertGreaterEqual(nginx.count('deny all;'), 2)
        self.assertGreaterEqual(nginx.count('access_log off;'), 2)
        self.assertIn('proxy_set_header Authorization $http_authorization;', nginx)
        self.assertIn('proxy_read_timeout 125s;', nginx)
        self.assertIn('unix:/run/jdlg-content-capture/capture.sock:/v1/capture', nginx)
        self.assertIn('unix:/run/jdlg-content-capture/capture.sock:/health/ready', nginx)

    def test_install_never_overwrites_extension_or_changes_global_userns_policy(self):
        deploy = self.read('deploy.sh')
        self.assertIn('NGINX_EXTENSION_DIR=', deploy)
        self.assertIn('test ! -e "$NGINX_TARGET"', deploy)
        self.assertIn('nginx -t', deploy)
        self.assertIn('systemctl reload nginx', deploy)
        deployment_files = [
            ROOT / 'deploy.sh',
            ROOT / 'rollback.sh',
            ROOT / 'verify.sh',
            ROOT / 'stale_socket.py',
            *sorted((ROOT / 'systemd').iterdir()),
        ]
        combined = '\n'.join(path.read_text() for path in deployment_files)
        self.assertNotRegex(combined, re.compile(r'sysctl\s+.*unprivileged_userns', re.I))
        self.assertNotIn('/etc/hosts', combined)

    def test_install_supports_repo_source_and_prestaged_target_layouts(self):
        deploy = self.read('deploy.sh')
        self.assertIn('dirname -- "$0"', deploy)
        self.assertIn('[ "$SOURCE_DIR" = "$ROOT/ops" ]', deploy)
        self.assertIn('OPS_PRESTAGED=true', deploy)
        self.assertIn('[ "$OPS_PRESTAGED" = true ] || touch "$OWNED/ops"', deploy)

    def test_browser_dependencies_and_version_are_checked_as_the_service_user(self):
        deploy = self.read('deploy.sh')
        self.assertIn('runuser -u jdlgcap-browser -- ldd "$BROWSER_BINARY"', deploy)
        self.assertIn("awk '/=> not found/ {print $1}'", deploy)
        self.assertIn('runuser -u jdlgcap-browser -- "$BROWSER_BINARY" --version', deploy)
        self.assertNotIn('"$BROWSER_BINARY" --version |', deploy)

    def test_rollback_requires_ownership_and_removes_only_dedicated_resources(self):
        rollback = self.read('rollback.sh')
        self.assertIn('test -d "$OWNED"', rollback)
        self.assertIn('jdlg-content-capture-network-owned.json', rollback)
        self.assertNotIn('$ROOT/browser', rollback)
        self.assertNotIn('$ROOT/bin', rollback)
        self.assertNotIn('rm -rf', rollback)

    def test_apparmor_exception_is_bound_to_the_exact_browser_binary(self):
        profile = self.read('apparmor/jdlg-content-capture-browser')
        self.assertIn('/opt/jdlg-content-capture/browser/chrome-headless-shell-linux64/chrome-headless-shell', profile)
        self.assertIn('flags=(unconfined)', profile)
        self.assertIn('userns,', profile)


if __name__ == '__main__':
    unittest.main()
