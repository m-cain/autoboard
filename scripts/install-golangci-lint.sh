#!/usr/bin/env sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
version=$(tr -d '[:space:]' < "$repository_root/.golangci-version")
binary_directory="$repository_root/.tools/bin"
binary="$binary_directory/golangci-lint"

if test -x "$binary" && "$binary" --version | grep -F "version ${version#v}" >/dev/null
then
  exit 0
fi

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/autoboard-golangci-lint.XXXXXX")
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

curl --proto '=https' --tlsv1.2 --silent --show-error --fail --location \
  https://golangci-lint.run/install.sh \
  --output "$temporary_directory/install.sh"
mkdir -p "$binary_directory"
sh "$temporary_directory/install.sh" -b "$binary_directory" "$version"

if ! "$binary" --version | grep -F "version ${version#v}" >/dev/null
then
  echo "installed golangci-lint does not match $version" >&2
  exit 1
fi
