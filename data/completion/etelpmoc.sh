# shellcheck shell=bash
#
#  Copyright (C) 2017 Canonical Ltd
#
#  This program is free software: you can redistribute it and/or modify
#  it under the terms of the GNU General Public License version 3 as
#  published by the Free Software Foundation.
#
#  This program is distributed in the hope that it will be useful,
#  but WITHOUT ANY WARRANTY; without even the implied warranty of
#  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#  GNU General Public License for more details.
#
#  You should have received a copy of the GNU General Public License
#  along with this program.  If not, see <http://www.gnu.org/licenses/>.

# etelpmoc is the reverse of complete: it de-serialises the tab completion
# request into the appropriate environment variables expected by the tab
# completion tools, performs whatever action is wanted, and serialises the
# result. It accomplishes this by having functions override the builtin
# completion commands.
#
# this always runs "inside", in the same environment you get when doing "snap
# run --shell", and snap-exec is the one setting the first argument to the
# completion script set in the snap. The rest of the arguments come through
# from snap-run --command=complete <snap> <args...>

_die() {
    echo "$*" >&2
    exit 1
}

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
    _die "ERROR: this is meant to be run, not sourced."
fi

if [[ "${#@}" -lt 8 ]]; then
    _die "USAGE: $0 <script> <COMP_TYPE> <COMP_KEY> <COMP_POINT> <COMP_CWORD> <COMP_WORDBREAKS> <COMP_LINE> cmd [args...]"
fi

# De-serialize the command line arguments and populate tab completion environment
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

# Source the bash-completion library functions and common completion setup
# shellcheck disable=SC1091
. /usr/share/bash-completion/bash_completion
# Now source the snap's 'completer' script itself
# shellcheck disable=SC1090
. "$_compscript"

# _compopts is an associative array, which keys are options. The options are
# described in bash(1)'s description of the -o option to the "complete"
# builtin, and they affect how the completion options are presented to the user
# (e.g. adding a slash for directories, whether to add a space after the
# completion, etc). These need setting in the user's environment so need
# serializing separately from the completions themselves.
declare -A _compopts

# wrap compgen, setting _compopts for any options given.
# (as these options need handling separately from the completions)
compgen() {
    local opt

    while getopts :o: opt; do
        case "$opt" in
            o)
                _compopts["$OPTARG"]=1
                ;;
            *)
                # Do nothing, explicitly. This silences shellcheck's detector
                # of unhandled command line options.
                ;;
        esac
    done
    builtin compgen "$@"
}

# compopt replaces the original compopt with one that just sets/unsets entries
# in _compopts
compopt() {
    local i

    for ((i=0; i<$#; i++)); do
        # in bash, ${!x} does variable indirection. Thus if x=1, ${!x} becomes $1.
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
}

_compfunc="_minimal"
_compact=""
# this is a lot more complicated than it should be, but it's how you
# get the result of 'complete -p "$1"' into an array, splitting it as
# the shell would.
readarray -t _comp < <(xargs -n1 < <(complete -p "$1") )
# _comp is now an array of the appropriate 'complete' invocation, word-split as
# the shell would, so we can now inspect it with getopts to determine the
# appropriate completion action.
# Unfortunately shellcheck doesn't know about readarray:
# shellcheck disable=SC2154
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
                readarray -t COMPREPLY < <( builtin compgen -W "$OPTARG" -- "${COMP_WORDS[$COMP_CWORD]}" )
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
        readarray -t COMPREPLY < <( builtin compgen -A "$_compact" -- "${COMP_WORDS[$COMP_CWORD]}" )
    elif [ "$_compfunc" ]; then
        # execute completion function (or the command if -C)
        $_compfunc
    fi
fi

# print completions to stdout
echo "${!_compopts[@]}"
echo "$_bounce"
echo ""
printf "%s\\n" "${COMPREPLY[@]}"
