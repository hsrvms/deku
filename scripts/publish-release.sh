#!/usr/bin/env bash
set -euo pipefail
shopt -s nullglob

if [[ $# -ne 2 ]]; then
  printf 'usage: %s TAG DIST_DIR\n' "$0" >&2
  exit 2
fi

tag=$1
dist_dir=$2

if [[ ! -d "$dist_dir" ]]; then
  printf 'publish release: distribution directory %q does not exist\n' "$dist_dir" >&2
  exit 1
fi

archives=("$dist_dir"/deku_*.tar.gz "$dist_dir"/deku_*.zip)
if [[ "${#archives[@]}" -ne 5 || ! -f "$dist_dir/SHA256SUMS" ]]; then
  printf 'publish release: expected five archives and SHA256SUMS in %q\n' "$dist_dir" >&2
  exit 1
fi

release_exists=false
if gh release view "$tag" --json isDraft,isPrerelease >/dev/null 2>&1; then
  release_exists=true
  if gh release view "$tag" --json isPrerelease --jq '.isPrerelease' | grep -Fqx true; then
    printf 'publish release: existing release %q is a prerelease\n' "$tag" >&2
    exit 1
  fi
else
  gh release create "$tag" \
    --draft \
    --generate-notes \
    --title "$tag" \
    --verify-tag
fi

assets=("${archives[@]}" "$dist_dir/SHA256SUMS")
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT

for asset in "${assets[@]}"; do
  name=$(basename "$asset")
  if gh release view "$tag" --json assets --jq '.assets[].name' | grep -Fqx "$name"; then
    downloaded="$temporary/$name"
    gh release download "$tag" --pattern "$name" --dir "$temporary" --clobber
    if [[ ! -f "$downloaded" ]] || ! cmp -s "$asset" "$downloaded"; then
      printf 'publish release: existing asset %q has different bytes\n' "$name" >&2
      exit 1
    fi
    printf 'verified existing asset %s\n' "$name"
  else
    gh release upload "$tag" "$asset"
    printf 'uploaded asset %s\n' "$name"
  fi
done

if [[ "$release_exists" == false ]] || gh release view "$tag" --json isDraft --jq '.isDraft' | grep -Fqx true; then
  gh release edit "$tag" --draft=false
fi

printf 'published release %s\n' "$tag"
