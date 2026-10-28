#!/bin/sh
# Builds the Windows resource that gives each .exe its icon and its Properties
# tab, and puts it where the linker will find it.
#
# Run before a windows build and removed after it. The Go toolchain links any
# .syso in the package directory automatically, with no import and no build tag -
# which is convenient for windows and a trap for everything else: the release
# builds four platforms in one loop, in one working directory, so a file left
# behind would be linked into the next one. That is why this is a step with a
# matching removal rather than a file in the tree.
#
#   build/windows-resource.sh v1.2.3      write the resource of both programs
#   build/windows-resource.sh --clean     remove them again
#
# Two programs, two packages: the web binary in cmd/ and the terminal client in
# cmd/client/. A .syso belongs to its package directory and to no other, so each
# gets its own, from the one template with its own name and description put in -
# one template, so the icon, the copyright and the version are written once.
#
# The name matters. goversioninfo writes resource.syso by default, which the
# toolchain links for EVERY platform; the _windows_amd64 suffix is what confines
# it, the same way it confines a .go file.

set -eu

WEB="cmd/resource_windows_amd64.syso"
CLIENT="cmd/client/resource_windows_amd64.syso"

if [ "${1:-}" = "--clean" ]; then
  rm -f "$WEB" "$CLIENT"
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

# resource <target> <file name without .exe> <description>
resource() {
  sed -e "s/\"Major\": 0/\"Major\": ${MAJOR}/" \
      -e "s/\"Minor\": 0/\"Minor\": ${MINOR}/" \
      -e "s/\"Patch\": 0/\"Patch\": ${PATCH}/" \
      -e "s/0\.0\.0\.0/${MAJOR}.${MINOR}.${PATCH}.0/" \
      -e "s/\"FileDescription\": \"[^\"]*\"/\"FileDescription\": \"$3\"/" \
      -e "s/\"InternalName\": \"[^\"]*\"/\"InternalName\": \"$2\"/" \
      -e "s/\"OriginalFilename\": \"[^\"]*\"/\"OriginalFilename\": \"$2.exe\"/" \
      build/versioninfo.json.in > build/versioninfo.json

  goversioninfo -o "$1" build/versioninfo.json
  rm -f build/versioninfo.json

  printf 'wrote %s for %s\n' "$1" "$VERSION"
}

resource "$WEB" go-time-recording-web "Project time tracking: REST API and web interface in one file"
resource "$CLIENT" go-time-recording-client "Project time tracking: the terminal client"
