# -*- bash -*-

# _complete_from_snap performs the tab completion request by calling the
# appropriate 'snap run --command=complete' with serialized args, and
# deserializes the response into the usual tab completion result.
#
# How snap command completion works is:
# 1. snappy's complete.sh is sourced into the user's shell environment
# 2. user performs '<command> <tab>'. If '<command>' is a snap command,
#    proceed to step '3', otherwise perform normal bash completion
# 3. run 'snap run --command=complete ...', converting bash completion
#    environment into serialized command line arguments
# 4. 'snap run --command=complete ...' exec()s 'etelpmoc.sh' within the snap's
#    runtime environment and confinement
# 5. 'etelpmoc.sh' takes the serialized command line arguments from step '3'
#    and puts them back into the bash completion environment variables
# 6. 'etelpmoc.sh' sources the snap's 'completer' script, performs the bash
#    completion and serializes the resulting completion environment variables
#    by printing to stdout the results in a format that snappy's complete.sh
#    will understand, then exits
# 7. control returns to snappy's 'complete.sh' and it deserializes the output
#    from 'etelpmoc.sh', validates the results and puts the validated results
#    into the bash completion environment variables
# 8. bash displays the results to the user
_complete_from_snap() {
    {
        # De-serialize the output of 'snap run --command=complete ...' into the format
        # bash expects:
        read -a opts
        # opts is expected to be a series of compopt options
        if [[ ${#opts[@]} -gt 0 ]]; then
            if [[ "${opts[0]}" == "cannot" ]]; then
                # older snap-execs sent errors over stdout :-(
                return 1
            fi

            for i in "${opts[@]}"; do
                if ! [[ "$i" =~ ^[a-z]+$ ]]; then
                    # only lowercase alpha characters allowed
                    return 2
                fi
            done
        fi

        read bounced
        case "$bounced" in
            ""|"alias"|"export"|"job"|"variable")
                # OK
                ;;
            *)
                # unrecognised bounce
                return 2
                ;;
        esac

        read sep
        if [ -n "$sep" ]; then
            # non-blank separator? madness!
            return 2
        fi
        local oldIFS="$IFS"

        if [ ! "$bounced" ]; then
            local IFS=$'\n'
            # Ignore any suspicious results that are uncommon in filenames and that
            # might be used to trick the user. A whitelist approach would be better
            # but is impractical with UTF-8 and common characters like quotes.
            COMPREPLY=( $( command grep -v '[[:cntrl:];&?*{}]' ) )
            IFS="$oldIFS"
        fi

        if [[ ${#opts[@]} -gt 0 ]]; then
            # shellcheck disable=SC2046
            # (we *want* word splitting to happen here)
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
    if [[ "$(which "$1")" =~ /snap/bin/ && ( -e /var/lib/snapd/snap/core/current/usr/lib/snapd/etelpmoc.sh || -e /snap/core/current/usr/lib/snapd/etelpmoc.sh ) ]]; then
        _complete_from_snap "$1"
        return $?
    fi
    # fallback to the old -D
    _completion_loader "$1"
}

complete -D -F _complete_from_snap_maybe
