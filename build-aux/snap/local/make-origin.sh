#!/bin/sh

set -eux
package="${1}"
shift
origin="${package}=$(dpkg-parsechangelog -SVersion)"
setfattr -n user.craft_parts.origin_stage_package -v "${origin}" "$@"
