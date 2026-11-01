#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  echo "Usage: $0 --crypto-file ABSOLUTE_PATH --output-dir ABSOLUTE_PATH" >&2
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

crypto_file=''
output_dir=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --crypto-file) crypto_file=${2:-}; shift 2 ;;
    --output-dir) output_dir=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

if [ -z "$crypto_file" ] || [ -z "$output_dir" ]; then
  usage
  exit 2
fi
case "$crypto_file" in /*) ;; *) echo 'crypto file must use an absolute path' >&2; exit 2 ;; esac
case "$output_dir" in /*) ;; *) echo 'output directory must use an absolute path' >&2; exit 2 ;; esac
if [ ! -f "$crypto_file" ] || [ -L "$crypto_file" ]; then
  echo 'crypto file must be a regular file, not a symlink' >&2
  exit 1
fi
if mode=$(stat -c '%a' "$crypto_file" 2>/dev/null); then :; else mode=$(stat -f '%Lp' "$crypto_file"); fi
case "$mode" in 400|600) ;; *) echo 'crypto file permissions must be 0400 or 0600' >&2; exit 1 ;; esac
if [ -e "$output_dir" ]; then
  echo 'output directory already exists' >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"
case "$output_dir" in "$repo_root"|"$repo_root"/*) echo 'output directory must be outside the repository' >&2; exit 1 ;; esac

hidden=''
while IFS= read -r entry; do
  flag=${entry%% *}
  file=${entry#* }
  case "$flag" in
    [a-z]|S)
      if [ "$file" != 'service/function/crypto.go' ]; then
        hidden="${hidden}${file}\n"
      fi
      ;;
  esac
done < <(git ls-files -v)
if [ -n "$hidden" ]; then
  echo 'unexpected hidden tracked files:' >&2
  printf '%b' "$hidden" >&2
  exit 1
fi

dirty=$(git status --porcelain=v1 --untracked-files=all -- . ':(exclude)service/function/crypto.go')
if [ -n "$dirty" ]; then
  echo 'working tree contains changes outside the protected crypto source:' >&2
  printf '%s\n' "$dirty" >&2
  exit 1
fi

if grep -Eq 'REPLACE_SM4_KEY!|REPLACE_WITH_LOCAL_HMAC_KEY' "$crypto_file"; then
  echo 'protected crypto source still contains release placeholders' >&2
  exit 1
fi
command -v node >/dev/null 2>&1 || { echo 'node is required for the client-to-server auth contract' >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo 'go is required for the backend build' >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo 'python3 is required for the final ELF auth verification' >&2; exit 1; }

commit=$(git rev-parse HEAD)
tree=$(git rev-parse 'HEAD^{tree}')
stage=$(mktemp -d "${TMPDIR:-/tmp}/jdlg-protected-release.XXXXXX")
trap 'rm -rf "$stage"' EXIT
git archive --format=tar "$commit" | tar -xf - -C "$stage"
install -m 600 "$crypto_file" "$stage/service/function/crypto.go"
crypto_sha256=$(sha256_file "$stage/service/function/crypto.go")

(
  cd "$stage/service"
  JDLG_REQUIRE_CLIENT_AUTH_CONTRACT=1 JDLG_AUTH_CRYPTO_SHA256="$crypto_sha256" CGO_ENABLED=0 go test ./route \
    -run '^TestMiniprogramClientSigner(?:Defaults|ExplicitDefaults)PassTokenAuthRequired$' -count=1
  CGO_ENABLED=0 GOOS=${GOOS:-linux} GOARCH=${GOARCH:-amd64} \
    go build -trimpath -o "$stage/service-bin" .
)
python3 "$stage/service/scripts/verify-auth-elf.py" \
  --binary "$stage/service-bin" --source "$stage/service/function/crypto.go" >/dev/null

binary_sha256=$(sha256_file "$stage/service-bin")
built_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
go_version=$(go env GOVERSION)
mkdir -m 700 "$output_dir"
install -m 700 "$stage/service-bin" "$output_dir/service"
cat >"$output_dir/auth-build.json" <<EOF
{
  "schema_version": 1,
  "commit": "$commit",
  "tree": "$tree",
  "crypto_sha256": "$crypto_sha256",
  "binary_sha256": "$binary_sha256",
  "auth_binary_hmac_match": true,
  "auth_binary_sm4_match": true,
  "go_version": "$go_version",
  "goos": "${GOOS:-linux}",
  "goarch": "${GOARCH:-amd64}",
  "built_at_utc": "$built_at"
}
EOF
chmod 600 "$output_dir/auth-build.json"

echo "protected release built: $output_dir"
