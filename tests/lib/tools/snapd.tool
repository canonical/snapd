#!/bin/sh

show_usage() {
	echo "usage: snapd.tool [OPTIONS] exec <TOOL> [ARGS]"
}

show_help() {
	show_usage
	echo
	echo "Available options:"
	echo "	-h --help	show this help message."
	echo
	echo "The snapd.tool program simplifies running internal tools,"
	echo "like snap-discard-ns, which are not on PATH and whose"
	echo "location varies from one distribution to another"
}

tool=""
while [ $# -gt 0 ]; do
	case "${1:-}" in
		-h|--help)
			show_help
			exit 0
			;;
		exec)
			shift
			tool="${1:-}"  # empty value checked below
			shift
			break
			;;
		--)
			shift
			break
			;;
		-*)
			echo "snapd.tool: unsupported argument $1" >&2
			show_usage
			exit 1
			;;
		*)
			echo "snapd.tool: unsupported argument $1" >&2
			show_usage
			exit 1
			;;
	esac
done

if [ "$tool" = "" ]; then
	show_usage
	exit 1
fi

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"
exec "$LIBEXECDIR/snapd/$tool" "$@"
