#!/usr/bin/env bash
set -euo pipefail

# Run only the template update contract. Copy production sources unchanged so
# local partial checkouts and the complete repository exercise identical code.
# This is not a substitute for building the entire service or MySQL integration.
service_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/jdlg-template-contract.XXXXXX")"
trap 'rm -rf -- "$test_dir"' EXIT

mkdir -p "$test_dir/model" "$test_dir/route"
cp "$service_dir/go.mod" "$service_dir/go.sum" "$test_dir/"
for file in template.go template_update.go image_metadata.go inspiration_asset.go template_update_test.go; do
  cp "$service_dir/model/$file" "$test_dir/model/$file"
done
for file in template_update_handler.go template_update_handler_test.go; do
  cp "$service_dir/route/$file" "$test_dir/route/$file"
done

cd "$test_dir"
export GOWORK=off
export GOTOOLCHAIN=local
go test -mod=readonly -race -count=1 -v ./model ./route
