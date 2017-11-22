#!/bin/bash

# Define MATCH when invoked outside of spread context.
#
# The MATCH function is like a super version of grep that prints the input when
# no match was found. It is used throughout snapd tests. This file is overwritten
# on project prepare but the function exists for reference and for aid to shellcheck.
MATCH() {
	{
		set +xu
	} 2> /dev/null
	[ ${#0} -gt 0 ] || {
		echo "error: missing regexp argument"
		return 1
	}
	local stdin
	stdin="$(cat)"
	echo "$stdin" | grep -q -E "$@" || {
		echo "error: pattern not found, got
$stdin" 1>&2
		return 1
	}
}

