#!/bin/sh
set -eu

NGINX_EXTENSION_DIR=
[ "$#" -eq 2 ] && [ "$1" = "--nginx-extension-dir" ] || {
  echo "usage: $0 --nginx-extension-dir /absolute/existing/directory" >&2
  exit 2
}
NGINX_EXTENSION_DIR=$2
case "$NGINX_EXTENSION_DIR" in
  /*) ;;
  *) exit 2 ;;
esac

test "$(id -u)" -eq 0
ROOT=/opt/jdlg-content-capture
OWNED=$ROOT/.deployment-owned
test -d "$OWNED" && test ! -L "$OWNED"
test "$(cat "$OWNED/nginx-extension-dir")" = "$NGINX_EXTENSION_DIR"
NGINX_TARGET=$NGINX_EXTENSION_DIR/jdlg-content-capture.conf

systemctl disable --now jdlg-content-capture-network-refresh.timer 2>/dev/null || true
systemctl disable --now jdlg-content-capture-worker.service 2>/dev/null || true
systemctl disable --now jdlg-content-capture-browser.service 2>/dev/null || true
systemctl disable --now jdlg-content-capture-network.service 2>/dev/null || true
if [ -e /run/jdlg-content-capture-network-owned.json ]; then
  /opt/jdlg-content-capture/ops/network.py down
fi

if [ -e "$OWNED/nginx" ]; then
  rm -f "$NGINX_TARGET"
  if nginx -t; then
    systemctl reload nginx
  else
    echo 'nginx validation failed after removing the owned extension; nginx was not reloaded' >&2
  fi
fi

if [ -e "$OWNED/units" ]; then
  rm -f /etc/systemd/system/jdlg-content-capture-network.service \
    /etc/systemd/system/jdlg-content-capture-browser.service \
    /etc/systemd/system/jdlg-content-capture-worker.service \
    /etc/systemd/system/jdlg-content-capture-network-refresh.service \
    /etc/systemd/system/jdlg-content-capture-network-refresh.timer
  systemctl daemon-reload
fi

if [ -e "$OWNED/apparmor" ]; then
  apparmor_parser -R /etc/apparmor.d/jdlg-content-capture-browser 2>/dev/null || true
  rm -f /etc/apparmor.d/jdlg-content-capture-browser
fi
[ ! -e "$OWNED/resolver" ] || rm -f /etc/netns/jdlg-content-capture/resolv.conf
[ ! -e "$OWNED/resolver" ] || rmdir /etc/netns/jdlg-content-capture 2>/dev/null || true
[ ! -e "$OWNED/worker-env" ] || rm -f "$ROOT/worker.env"
[ ! -e "$OWNED/main-env" ] || rm -f "$ROOT/main-service.env"
if [ -e "$OWNED/ops" ]; then
  rm -f "$ROOT/ops/network.py" "$ROOT/ops/stale_socket.py" "$ROOT/ops/rollback.sh"
  rmdir "$ROOT/ops" 2>/dev/null || true
fi
[ ! -e "$OWNED/worker-user" ] || userdel jdlgcap-worker 2>/dev/null || true
[ ! -e "$OWNED/browser-user" ] || userdel jdlgcap-browser 2>/dev/null || true
rm -f "$OWNED/nginx-extension-dir" "$OWNED/nginx" "$OWNED/units" \
  "$OWNED/apparmor" "$OWNED/resolver" "$OWNED/worker-env" "$OWNED/main-env" "$OWNED/ops" \
  "$OWNED/worker-user" "$OWNED/browser-user" "$OWNED/browser-ldd-report"
rmdir "$OWNED"
echo 'owned content capture deployment resources were removed'
