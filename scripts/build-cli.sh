#!/bin/bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

cli_version=$(plutil -extract version raw -o - mygo.json)
shared_resources="$repo_root/resources/darwin"
mkdir -p "$shared_resources"
rm -f "$shared_resources/sheaf"
binary="$repo_root/resources/darwin-arm64/sheaf"
mkdir -p "$(dirname -- "$binary")"
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X sheaf/internal/cli.version=$cli_version" -o "$binary" ./cmd/sheaf
codesign --force --sign - "$binary"
# The script names the command after argv[0], so run it as "sheaf".
(exec -a sheaf "$binary" completion zsh) > "$shared_resources/_sheaf"
grep -qx "#compdef sheaf" "$shared_resources/_sheaf"
