#!/bin/bash

show_help() {
	echo "usage: snapd.tool [OPTIONS] exec <TOOL> [ARGS]"
	echo
	echo "Available options:"
	echo "	-h --help	show this help message."
	echo
	echo "The snapd.tool program simplifies running internal tools,"
	echo "like snap-discard-ns, which are not on PATH and whose"
	echo "location varies from one distribution to another"
}

main() {
	if [ $# -eq 0 ]; then
        show_help
        exit 0
    fi

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
				show_help
				exit 1
				;;
			*)
				echo "snapd.tool: unsupported argument $1" >&2
				show_help
				exit 1
				;;
		esac
	done

	# shellcheck source=tests/lib/dirs.sh
	. "$TESTSLIB/dirs.sh"
	exec "$LIBEXECDIR/snapd/$tool" "$@"
}

main "$@"