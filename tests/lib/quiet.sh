#!/bin/bash

# use "quiet foo" when you expect "foo" to produce a lot of output
# that isn't useful unless foo itself fails.
quiet() (
    # note this is a subshell (parens instead of braces around the function)
    # so this set only affects this function and not the caller
    { set +x; } >&/dev/null

    # not strictly needed because it's a subshell, but good practice
    local tf retval

    tf="$(tempfile)"

    set +e
    "$@" >& "$tf"
    retval=$?
    set -e

    if [ "$retval" != "0" ]; then
        echo "quiet: $*" >&2
        echo "quiet: exit status $retval. Output follows:" >&2
        cat "$tf" >&2
        echo "quiet: end of output." >&2
    fi

    rm -f -- "$tf"

    return $retval
)
