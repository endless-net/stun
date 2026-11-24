#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
cd -- "$root"
export GOWORK=off GOFLAGS=-mod=readonly
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT

# Build the same source archive used by an API release, without creating a tag.
go run ./scripts/ci build --tag api/v0.0.0 --out "$scratch/release"
mkdir -- "$scratch/contract" "$scratch/built"
git archive HEAD -- api | tar -xf - -C "$scratch/contract"
tar -xzf "$scratch/release/stun-api-api-v0.0.0-source-any.tar.gz" -C "$scratch/built"
diff -qr "$scratch/contract/api" "$scratch/built"
printf 'API build matches contract at %s\n' "$(git -C "$root" rev-parse HEAD)"
