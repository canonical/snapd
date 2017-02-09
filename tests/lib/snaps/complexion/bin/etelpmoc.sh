#!/bin/bash

_die() {
    echo "$*" >&2
    exit 1
}

if [[ "$BASH_SOURCE" != "$0" ]]; then
    _die "ERROR: this is meant to be run, not sourced."
fi

if [[ "${#@}" -lt 5 ]]; then
    _die "USAGE: $0 <script> <COMP_TYPE> <COMP_POINT> <COMP_CWORD> cmd [args...]"
fi

. /usr/share/bash-completion/bash_completion

_compscript="$1"
shift
COMP_TYPE="$1"
shift
COMP_POINT="$1"
shift
COMP_CWORD="$1"
shift
# rest of the args is the command itself
COMP_LINE="$*"
COMP_WORDS=("$@")

COMPREPLY=()

if [[ ! "$_compscript" ]]; then
    _die "ERROR: completion script filename can't be empty"
fi
if [[ ! -f "$_compscript" ]]; then
    _die "ERROR: completion script does not exist"
fi

. $_compscript

declare -A _compopts
_compfunc="_minimal"
# NOTE we only really know how to complete things that use functions
_comp=( $(complete -p "$1") )
if [[ "${_comp[*]}" ]]; then
    while getopts :o:F: opt "${_comp[@]:1}"; do
        case "$opt" in
            o)
                _compopts["$OPTARG"]=1
                ;;
            F)
                _compfunc="$OPTARG"
                ;;
            *)
                _die "ERROR: unknown option -$opt (we only know about -o and -F for now)"
                ;;
        esac
    done
fi

xcompgen() {
    local opt

    while getopts :o: opt; do
        case "$opt" in
            o)
                _compopts["$OPTARG"]=1
                ;;
        esac
    done
    \compgen "$@"
}
alias compgen=xcompgen

compopt() (
    local i

    for ((i=0; i<$#; i++)); do
        case "${!i}" in
            -o)
                ((i++))
                _compopts[${!i}]=1
                ;;
            +o)
                ((i++))
                unset _compopts[${!i}]
                ;;
        esac
    done
)

# execute completion function
"$_compfunc"

# print completions to stdout
echo "${!_compopts[@]}"
printf "%s\n" "${COMPREPLY[@]}"
