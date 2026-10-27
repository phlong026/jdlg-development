#!/bin/sh
set -eu

SOURCE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)

python3 -m unittest \
  "$SOURCE_DIR/test_network.py" \
  "$SOURCE_DIR/test_stale_socket.py" \
  "$SOURCE_DIR/test_deployment_policy.py"
python3 -m py_compile "$SOURCE_DIR/network.py" "$SOURCE_DIR/stale_socket.py"
sh -n "$SOURCE_DIR/deploy.sh" "$SOURCE_DIR/rollback.sh" "$SOURCE_DIR/verify.sh"

if [ "${1:-}" = "--installed" ]; then
  test "$(id -u)" -eq 0
  systemd-analyze verify --man=no /etc/systemd/system/jdlg-content-capture-*.service /etc/systemd/system/jdlg-content-capture-*.timer
  systemctl is-active --quiet jdlg-content-capture-network.service
  systemctl is-active --quiet jdlg-content-capture-browser.service
  systemctl is-active --quiet jdlg-content-capture-worker.service
  systemctl is-active --quiet jdlg-content-capture-network-refresh.timer
  test "$(stat -c '%a:%U:%G' /opt/jdlg-content-capture/worker.env)" = "600:root:root"
  test "$(stat -c '%a:%U:%G' /run/jdlg-content-capture/capture.sock)" = "660:jdlgcap-worker:www"
  ip netns exec jdlg-content-capture ss -lnt | grep -Eq '127\.0\.0\.1:9222[[:space:]]'

  SECRET=$(sed -n 's/^CONTENT_CAPTURE_SECRET=//p' /opt/jdlg-content-capture/worker.env)
  test "${#SECRET}" -ge 32
  CURL_CONFIG=$(mktemp /run/jdlg-content-capture-verify.XXXXXX)
  trap 'rm -f "$CURL_CONFIG"' EXIT HUP INT TERM
  chmod 600 "$CURL_CONFIG"
  {
    printf '%s\n' 'silent' 'show-error' 'fail'
    printf 'header = "Authorization: Bearer %s"\n' "$SECRET"
    printf '%s\n' 'resolve = "api.jiadilingguang.com:443:127.0.0.1"'
    printf '%s\n' 'url = "https://api.jiadilingguang.com/_internal/content-capture/ready"'
  } >"$CURL_CONFIG"
  BODY=$(curl --noproxy '*' --max-time 10 --config "$CURL_CONFIG")
  test "$BODY" = '{"ready":true}'
fi
