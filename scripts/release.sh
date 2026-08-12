#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT_DIR/dist}"
BIN_NAME="${BIN_NAME:-larkctl}"
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"

PLATFORMS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
  "windows amd64"
)

mkdir -p "$DIST_DIR"
rm -f "$DIST_DIR"/"${BIN_NAME}"_*.tar.gz "$DIST_DIR"/"${BIN_NAME}"_*.zip \
  "$DIST_DIR"/"${BIN_NAME}"-*-* "$DIST_DIR"/checksums.txt

for item in "${PLATFORMS[@]}"; do
  read -r goos goarch <<<"$item"

  package_base="${BIN_NAME}_${VERSION}_${goos}_${goarch}"
  workdir="${DIST_DIR}/${package_base}"
  mkdir -p "$workdir"

  bin_path="${workdir}/${BIN_NAME}"
  if [[ "$goos" == "windows" ]]; then
    bin_path="${bin_path}.exe"
  fi

  echo "==> build ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "$bin_path" "$ROOT_DIR"
  cp "$ROOT_DIR/docs/INSTALL.md" "$workdir/INSTALL.md"

  # Raw binary alongside the archive — `larkctl upgrade` downloads these names
  # directly from the release assets.
  raw_bin="${DIST_DIR}/${BIN_NAME}-${goos}-${goarch}"
  if [[ "$goos" == "windows" ]]; then
    raw_bin="${raw_bin}.exe"
  fi
  cp "$bin_path" "$raw_bin"

  pushd "$DIST_DIR" >/dev/null
  if [[ "$goos" == "windows" ]]; then
    zip -rq "${package_base}.zip" "${package_base}"
  else
    tar -czf "${package_base}.tar.gz" "${package_base}"
  fi
  popd >/dev/null

  rm -rf "$workdir"
done

pushd "$DIST_DIR" >/dev/null
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "${BIN_NAME}"_*.tar.gz "${BIN_NAME}"_*.zip "${BIN_NAME}"-*-* > checksums.txt
else
  shasum -a 256 "${BIN_NAME}"_*.tar.gz "${BIN_NAME}"_*.zip "${BIN_NAME}"-*-* > checksums.txt
fi
popd >/dev/null

echo "Release packages generated in: $DIST_DIR"
