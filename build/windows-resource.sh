#!/bin/sh
# Builds the Windows resource that gives the .exe its icon and its Properties
# tab, and puts it where the linker will find it.
#
# Run before a windows build and removed after it. The Go toolchain links any
# .syso in the package directory automatically, with no import and no build tag -
# which is convenient for windows and a trap for everything else: the release
# builds four platforms in one loop, in one working directory, so a file left
# behind would be linked into the next one. That is why this is a step with a
# matching removal rather than a file in the tree.
#
#   build/windows-resource.sh v1.2.3      write cmd/resource_windows_amd64.syso
#   build/windows-resource.sh --clean     remove it again
#
# The name matters. goversioninfo writes resource.syso by default, which the
# toolchain links for EVERY platform; the _windows_amd64 suffix is what confines
# it, the same way it confines a .go file.

set -eu

TARGET="cmd/resource_windows_amd64.syso"

if [ "${1:-}" = "--clean" ]; then
  rm -f "$TARGET"
  exit 0
fi

VERSION="${1:?usage: windows-resource.sh <version|--clean>}"

# Windows wants four numbers and the tags here are v1.2.3, so the v goes and the
# fourth part is zero. A version it cannot parse is not an error anybody sees -
# the field simply reads 0.0.0.0 in the dialog, which is worse than loud.
NUMBERS="$(printf '%s' "$VERSION" | sed 's/^v//' | cut -d- -f1)"
MAJOR="$(printf '%s' "$NUMBERS" | cut -d. -f1)"
MINOR="$(printf '%s' "$NUMBERS" | cut -d. -f2)"
PATCH="$(printf '%s' "$NUMBERS" | cut -d. -f3)"

# A tag that is not a version at all - a bare sha from a local build - still has
# to produce something the resource compiler accepts.
case "${MAJOR:-}${MINOR:-}${PATCH:-}" in
  *[!0-9]*|"") MAJOR=0; MINOR=0; PATCH=0 ;;
esac

sed -e "s/\"Major\": 0/\"Major\": ${MAJOR}/" \
    -e "s/\"Minor\": 0/\"Minor\": ${MINOR}/" \
    -e "s/\"Patch\": 0/\"Patch\": ${PATCH}/" \
    -e "s/0\.0\.0\.0/${MAJOR}.${MINOR}.${PATCH}.0/" \
    build/versioninfo.json.in > build/versioninfo.json

goversioninfo -o "$TARGET" build/versioninfo.json
rm -f build/versioninfo.json

printf 'wrote %s for %s\n' "$TARGET" "$VERSION"
