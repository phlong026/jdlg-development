#!/bin/sh
set -eu

SOURCE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
NGINX_EXTENSION_DIR=

usage() {
  echo "usage: $0 --nginx-extension-dir /absolute/existing/directory" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
[ "$1" = "--nginx-extension-dir" ] || usage
NGINX_EXTENSION_DIR=$2
case "$NGINX_EXTENSION_DIR" in
  /*) ;;
  *) usage ;;
esac
[ -d "$NGINX_EXTENSION_DIR" ] && [ ! -L "$NGINX_EXTENSION_DIR" ] || usage

test "$(id -u)" -eq 0
"$SOURCE_DIR/verify.sh"

for command in apparmor_parser awk curl getent grep ip ldd mktemp nginx openssl python3 runuser sed ss stat systemctl systemd-analyze useradd userdel; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing command: $command" >&2; exit 1; }
done
getent group www >/dev/null

ROOT=/opt/jdlg-content-capture
OWNED=$ROOT/.deployment-owned
BROWSER_BINARY=$ROOT/browser/chrome-headless-shell-linux64/chrome-headless-shell
NGINX_TARGET=$NGINX_EXTENSION_DIR/jdlg-content-capture.conf
APPARMOR_TARGET=/etc/apparmor.d/jdlg-content-capture-browser
OPS_PRESTAGED=false

test ! -e "$OWNED"
test -x "$BROWSER_BINARY"
test -x "$ROOT/bin/content-capture-worker"
test "$(stat -c '%U' "$BROWSER_BINARY")" = root
test "$(stat -c '%U' "$ROOT/bin/content-capture-worker")" = root
test "$((0$(stat -c '%a' "$BROWSER_BINARY") & 0022))" -eq 0
test "$((0$(stat -c '%a' "$ROOT/bin/content-capture-worker") & 0022))" -eq 0
test ! -e "$ROOT/worker.env"
test ! -e "$ROOT/main-service.env"
if [ "$SOURCE_DIR" = "$ROOT/ops" ]; then
  OPS_PRESTAGED=true
  for file in network.py stale_socket.py rollback.sh; do
    test -f "$SOURCE_DIR/$file" && test ! -L "$SOURCE_DIR/$file"
    test "$(stat -c '%U' "$SOURCE_DIR/$file")" = root
    test "$((0$(stat -c '%a' "$SOURCE_DIR/$file") & 0022))" -eq 0
  done
else
  test ! -e "$ROOT/ops/network.py"
  test ! -e "$ROOT/ops/stale_socket.py"
  test ! -e "$ROOT/ops/rollback.sh"
fi
test ! -e "$NGINX_TARGET"
test ! -e "$APPARMOR_TARGET"
test ! -e /etc/netns/jdlg-content-capture
for unit in "$SOURCE_DIR"/systemd/*; do
  test ! -e "/etc/systemd/system/$(basename "$unit")"
done
! getent passwd jdlgcap-browser >/dev/null
! getent passwd jdlgcap-worker >/dev/null
! getent group jdlgcap-browser >/dev/null

apparmor_parser -Q "$SOURCE_DIR/apparmor/jdlg-content-capture-browser"

install -d -m 0700 -o root -g root "$OWNED"
printf '%s\n' "$NGINX_EXTENSION_DIR" >"$OWNED/nginx-extension-dir"

rollback_on_error() {
  status=$?
  trap - EXIT HUP INT TERM
  "$SOURCE_DIR/rollback.sh" --nginx-extension-dir "$NGINX_EXTENSION_DIR" || true
  exit "$status"
}
trap rollback_on_error EXIT HUP INT TERM

useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin jdlgcap-browser
touch "$OWNED/browser-user"
useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin --gid www jdlgcap-worker
touch "$OWNED/worker-user"

umask 077
LDD_REPORT=$OWNED/browser-ldd-report
if ! runuser -u jdlgcap-browser -- ldd "$BROWSER_BINARY" >"$LDD_REPORT" 2>&1; then
  echo 'browser dependency inspection failed' >&2
  exit 1
fi
MISSING_LIBRARIES=$(awk '/=> not found/ {print $1}' "$LDD_REPORT")
rm -f "$LDD_REPORT"
if [ -n "$MISSING_LIBRARIES" ]; then
  printf 'missing browser libraries:\n%s\n' "$MISSING_LIBRARIES" >&2
  exit 1
fi
if ! BROWSER_VERSION=$(runuser -u jdlgcap-browser -- "$BROWSER_BINARY" --version 2>/dev/null); then
  echo 'browser version check failed for the service user' >&2
  exit 1
fi
printf '%s\n' "$BROWSER_VERSION" | grep -F '156.0.8078.47' >/dev/null || {
  echo 'unexpected browser version' >&2
  exit 1
}
systemd-analyze verify --man=no "$SOURCE_DIR"/systemd/*.service "$SOURCE_DIR"/systemd/*.timer

if [ "$OPS_PRESTAGED" = false ]; then
  install -d -m 0755 -o root -g root "$ROOT/ops"
  install -m 0755 -o root -g root "$SOURCE_DIR/network.py" "$ROOT/ops/network.py"
  install -m 0755 -o root -g root "$SOURCE_DIR/stale_socket.py" "$ROOT/ops/stale_socket.py"
  install -m 0755 -o root -g root "$SOURCE_DIR/rollback.sh" "$ROOT/ops/rollback.sh"
fi
[ "$OPS_PRESTAGED" = true ] || touch "$OWNED/ops"

SECRET=$(openssl rand -hex 32)
{
  printf 'CONTENT_CAPTURE_SECRET=%s\n' "$SECRET"
  grep -v '^#' "$SOURCE_DIR/worker.env.example"
} >"$ROOT/worker.env"
chown root:root "$ROOT/worker.env"
chmod 0600 "$ROOT/worker.env"
touch "$OWNED/worker-env"

{
  printf '%s\n' 'CONTENT_IMPORT_CAPTURE_URL=https://api.jiadilingguang.com/_internal/content-capture/v1/capture'
  printf '%s\n' 'CONTENT_IMPORT_CAPTURE_HEALTH_URL=https://api.jiadilingguang.com/_internal/content-capture/ready'
  printf '%s\n' 'CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS=127.0.0.1:443'
  printf 'CONTENT_IMPORT_CAPTURE_SECRET=%s\n' "$SECRET"
} >"$ROOT/main-service.env"
chown root:root "$ROOT/main-service.env"
chmod 0600 "$ROOT/main-service.env"
touch "$OWNED/main-env"

install -d -m 0755 -o root -g root /etc/netns/jdlg-content-capture
printf '%s\n' 'nameserver 119.29.29.29' >/etc/netns/jdlg-content-capture/resolv.conf
chown root:root /etc/netns/jdlg-content-capture/resolv.conf
chmod 0644 /etc/netns/jdlg-content-capture/resolv.conf
touch "$OWNED/resolver"

install -m 0644 -o root -g root "$SOURCE_DIR/apparmor/jdlg-content-capture-browser" "$APPARMOR_TARGET"
apparmor_parser -r "$APPARMOR_TARGET"
touch "$OWNED/apparmor"

for unit in "$SOURCE_DIR"/systemd/*; do
  install -m 0644 -o root -g root "$unit" "/etc/systemd/system/$(basename "$unit")"
done
touch "$OWNED/units"
systemctl daemon-reload

install -m 0644 -o root -g root "$SOURCE_DIR/nginx/content-capture.conf" "$NGINX_TARGET"
touch "$OWNED/nginx"
if ! nginx -t; then
  rm -f "$NGINX_TARGET" "$OWNED/nginx"
  nginx -t || true
  exit 1
fi
systemctl reload nginx

systemctl enable --now jdlg-content-capture-network.service
systemctl enable --now jdlg-content-capture-browser.service
systemctl enable --now jdlg-content-capture-worker.service
systemctl enable --now jdlg-content-capture-network-refresh.timer

"$SOURCE_DIR/verify.sh" --installed
trap - EXIT HUP INT TERM
echo 'content capture deployment is ready'
