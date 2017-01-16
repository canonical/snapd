# use "quiet foo" when you expect "foo" to produce a lot of output
# that isn't useful unless foo itself fails.
quiet() (
    # note this is a subshell (parens instead of braces around the function)
    # so this set only affects this function itself
    { set +x; } >&/dev/null

    tf="$(tempfile)"
    if "$@" >& "$tf"; then
        rm -f -- "$tf"
        return 0
    else
        echo "Q: $*" >&2
        cat "$tf" >&2
        rm -f -- "$tf"
        return 1
    fi
)
