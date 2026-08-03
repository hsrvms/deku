#!/usr/bin/env bash
set -euo pipefail

version="${1:-0.0.0}"
root=$(git rev-parse --show-toplevel)
cd "$root"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT

input_dir="$temporary/build"
output_dir="$temporary/dist"
mkdir -p "$input_dir"

for target in \
  linux-amd64 \
  linux-arm64 \
  darwin-amd64 \
  darwin-arm64 \
  windows-amd64
 do
  os=${target%-*}
  arch=${target#*-}
  executable="deku-${target}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath \
      -ldflags "-s -w -X github.com/hsrvms/deku/version.Build=$version" \
      -o "$input_dir/$executable" \
      ./cmd/deku/
done

go run ./cmd/deku-release \
  -tag "v$version" \
  -input-dir "$input_dir" \
  -output-dir "$output_dir"

(cd "$output_dir" && sha256sum -c SHA256SUMS)

archives=("$output_dir"/deku_*.tar.gz "$output_dir"/deku_*.zip)
if [[ "${#archives[@]}" -ne 5 ]]; then
  printf 'release dry run: got %d archives, want 5\n' "${#archives[@]}" >&2
  exit 1
fi

printf 'release dry run passed for v%s\n' "$version"
