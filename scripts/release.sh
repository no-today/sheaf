#!/bin/bash
set -euo pipefail

if [[ $(uname -s) != Darwin ]]; then
  echo "Building a Sheaf release requires macOS." >&2
  exit 1
fi

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

output="$repo_root/build/release"
go tool mygo build -platform darwin/arm64 -skip-notarize -sign - -o "$output"

shopt -s nullglob
platform_dir="$output/darwin-arm64"
apps=("$platform_dir/"*.app)
dmgs=("$platform_dir/"*.dmg)
if [[ ${#apps[@]} -ne 1 || ${#dmgs[@]} -ne 1 ]]; then
  echo "Expected one app and one DMG in $platform_dir." >&2
  exit 1
fi

app=${apps[0]}
dmg=${dmgs[0]}
executable=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app/Contents/Info.plist")
for binary in "$app/Contents/MacOS/$executable" "$app/Contents/Resources/sheaf"; do
  actual_arch=$(lipo -archs "$binary")
  if [[ "$actual_arch" != "arm64" ]]; then
    echo "Expected only arm64 in $binary; got $actual_arch." >&2
    exit 1
  fi
done
codesign --verify --deep --strict --verbose=2 "$app"
hdiutil verify "$dmg"

printf '\nApp: %s\nDMG: %s\n' "$app" "$dmg"
codesign -dv "$app" 2>&1
name="sheaf-$(plutil -extract version raw -o - "$repo_root/mygo.json").dmg"
cp -- "$dmg" "$output/$name"
printf '\nDMG SHA-256:\n'
(cd -- "$output" && shasum -a 256 "$name") | tee "$output/$name.sha256"
