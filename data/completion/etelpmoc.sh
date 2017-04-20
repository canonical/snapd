#!/bin/bash

# etelpmoc is the backwards half of complete: it de-serialises the tab
# completion request into the appropriate environs expected by the tab
# completion tools, performs whatever action is wanted, and serialises the
# result. It accomplishes this by a mixture of aliases and functions overriding
# the builtin completion commands.

_die() {
    echo "$*" >&2
    exit 1
}

if [[ "$BASH_SOURCE" != "$0" ]]; then
    _die "ERROR: this is meant to be run, not sourced."
fi

if [[ "${#@}" -lt 8 ]]; then
    _die "USAGE: $0 <script> <COMP_TYPE> <COMP_KEY> <COMP_POINT> <COMP_CWORD> <COMP_WORDBREAKS> <COMP_LINE> cmd [args...]"
fi

_compscript="$1"
shift
COMP_TYPE="$1"
shift
COMP_KEY="$1"
shift
COMP_POINT="$1"
shift
COMP_CWORD="$1"
shift
COMP_WORDBREAKS="$1"
shift
# duplication, but whitespace is eaten and that throws off COMP_POINT
COMP_LINE="$1"
shift
# rest of the args is the command itself
COMP_WORDS=("$@")

COMPREPLY=()

if [[ ! "$_compscript" ]]; then
    _die "ERROR: completion script filename can't be empty"
fi
if [[ ! -f "$_compscript" ]]; then
    _die "ERROR: completion script does not exist"
fi

. /usr/share/bash-completion/bash_completion

. $_compscript

# _compopts is an associative array, which keys are options.
declare -A _compopts

# wrap compgen, setting _compopts for any options given.
xcompgen() {
    local opt

    while getopts :o: opt; do
        case "$opt" in
            o)
                _compopts["$OPTARG"]=1
                ;;
        esac
    done
    # aliases are not checked if the command is quoted, and a backslash counts.
    \compgen "$@"
}
alias compgen=xcompgen
shopt -s expand_aliases

# compopt replaces the original compopt with one that just sets/unsets entries
# in _compopts
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

_compfunc="_minimal"
_compact=""
# this is a lot more complicated than it should be, but it's how you
# get the result of 'complete -p "$1"' into an array, splitting it as
# the shell would.
readarray -t _comp < <(xargs -n1 < <(complete -p "$1") )
if [[ "${_comp[*]}" ]]; then
    while getopts :abcdefgjksuvA:C:W:o:F: opt "${_comp[@]:1}"; do
        case "$opt" in
            a)
                _compact="alias"
                ;;
            b)
                _compact="builtin"
                ;;
            c)
                _compact="command"
                ;;
            d)
                _compact="directory"
                ;;
            e)
                _compact="export"
                ;;
            f)
                _compact="file"
                ;;
            g)
                _compact="group"
                ;;
            j)
                _compact="job"
                ;;
            k)
                _compact="keyword"
                ;;
            s)
                _compact="service"
                ;;
            u)
                _compact="user"
                ;;
            v)
                _compact="variable"
                ;;
            A)
                _compact="$OPTARG"
                ;;
            o)
                _compopts["$OPTARG"]=1
                ;;
            C|F)
                _compfunc="$OPTARG"
                ;;
            W)
                readarray -t COMPREPLY < <( \compgen -W "$OPTARG" -- "${COMP_WORDS[$COMP_CWORD]}" )
                _compfunc=""
                ;;
            *)
                # P, G, S, and X are not supported yet
                _die "ERROR: unknown option -$OPTARG"
                ;;
        esac
    done
fi

_bounce=""
case "$_compact" in
    # these are for completing things that'll be interpreted by the
    # "outside" bash, so send them back to be completed there.
    "alias"|"export"|"job"|"variable")
        _bounce="$_compact"
        ;;
esac

if [ ! "$_bounce" ]; then
    if [ "$_compact" ]; then
        readarray -t COMPREPLY < <( \compgen -A "$_compact" -- "${COMP_WORDS[$COMP_CWORD]}" )
    elif [ "$_compfunc" ]; then
        # execute completion function (or the command if -C)
        $_compfunc
    fi
fi

# print completions to stdout
echo "${!_compopts[@]}"
echo "$_bounce"
echo ""
printf "%s\n" "${COMPREPLY[@]}"
