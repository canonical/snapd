# -*- bash -*-

# _complete_from_snap serialises the tab completion request and sends it off to
# the appropriate 'snap run --command=complete', and de-serialises the response
# into the usual tab completion result.
_complete_from_snap() {
    {
        read -a opts
        read bounced
        read sep
        if [ "$sep" ]; then
            # non-blank separator? madness!
            return 2
        fi
        local oldIFS="$IFS"

        if [ ! "$bounced" ]; then
            local IFS=$'\n'
            COMPREPLY=( $(cat) )
            IFS="$oldIFS"
        fi

        if [[ ${#opts[@]} -gt 0 ]]; then
            if [[ "${opts[0]}" == "cannot" ]]; then
                # older snap-execs sent errors over stdout :-(
                return 1
            fi
            compopt $(printf " -o %s" "${opts[@]}")
        fi
        if [ "$bounced" ]; then
            COMPREPLY+=(compgen -A "$bounced" -- "${COMP_WORDS[$COMP_CWORD]}")
        fi
    } < <(
        snap run --command=complete "$1" "$COMP_TYPE" "$COMP_KEY" "$COMP_POINT" "$COMP_CWORD" "$COMP_WORDBREAKS" "$COMP_LINE" "${COMP_WORDS[@]}" 2>/dev/null || return 1
    )

}

# _complete_from_snap_maybe calls _complete_from_snap if the command is in
# bin/snap, and otherwise does bash-completion's _completion_loader (which is
# what -D would've done before).
_complete_from_snap_maybe() {
    # catch /snap/bin and /var/lib/snapd/snap/bin
    if [[ "$(which "$1")" =~ /snap/bin/ ]]; then
        _complete_from_snap "$1"
        return $?
    fi
    # fallback to the old -D
    _completion_loader "$1"
}

complete -D -F _complete_from_snap_maybe
