#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

for script in scripts/*.sh; do
  bash -n "$script"
done

unformatted=$(find . -type f -name '*.go' -not -path './.git/*' -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
  echo "Go files require gofmt:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go mod verify
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/endlessnet-stun ./cmd/endlessnet-stun-smoke
