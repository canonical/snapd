#!/bin/bash
set -euo pipefail

# build-ci.sh - Minimal wrapper to build packages using kulturysta
#
# Orchestrates package builds by running kulturysta from the appropriate
# packaging/<distro>/ directory. Unifies local and CI/CD builds around a
# single source of truth (README.md with embedded build commands).
#
# Usage: packaging/build-ci.sh <distro> [--no-tests]
#
# Examples:
#   packaging/build-ci.sh debian-sid
#   packaging/build-ci.sh fedora-42 --no-tests

if [ $# -lt 1 ]; then
	echo "Usage: $0 <distro> [--no-tests]" >&2
	exit 1
fi

distro="$1"
shift || true  # Remove distro from args, keep any additional options

packaging_dir="$(dirname "$0")/$distro"

if [ ! -d "$packaging_dir" ]; then
	echo "Error: Distribution '$distro' not found" >&2
	exit 1
fi

if [ ! -f "$packaging_dir/README.md" ]; then
	echo "Error: README.md not found in $packaging_dir" >&2
	exit 1
fi

# Change to the distro directory and run kulturysta
cd "$packaging_dir"
../kulturysta "$@"
