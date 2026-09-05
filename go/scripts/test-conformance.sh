#!/bin/sh

set -eu

module_directory=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/k4k3ru-toml-conformance.XXXXXX")
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

decoder="$temporary_directory/decoder"

cd "$module_directory"
go build -o "$decoder" ./internal/tomltestcmd
go run github.com/toml-lang/toml-test/v2/cmd/toml-test@v2.2.0 \
	test \
	-toml=1.0 \
	-decoder="$decoder"
