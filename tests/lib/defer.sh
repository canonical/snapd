#!/bin/sh

# prime_defer schedules execution of deferred commands at script termination.
prime_defer() {
    scope=auto
    case "${1:-}" in
        --scope=*)
            scope=$(echo "$1" | cut -d = -f 2)
            shift
            ;;
    esac
    rm -f ".${scope}_defer_*"
    test -n "$SNAP_NO_CLEANUP" || trap "run_deferred --scope=\"$scope\"" EXIT
}

# defer enqueues execution of shell commands.
#
# synopsis: defer [--scope=SCOPE] <COMMANDS>
#
# Given commands are stored in a shell script called .$SCOPE_defer_$N where
# SCOPE is a name of the group of defer statements and N is an incrementing
# counter. The counter is stored in the file .$SCOPE_defer_count, protected
# from concurrent access by a flock-based lock file .defer_lock.
#
# Scope defaults to "auto", it exists to allow the prepare and restore code
# to use deferrals without interfering with the use in the execute section.
#
# For the prepare/restore sections please use --scope=prepare. For the execute
# section please use no scope at all. It is customary to use "trap run_deferred
# EXIT" inside the execute section, to allow deferrals to clean up background
# processes that would otherwise interfere with spread.
#
# To run all deferred commands use run_deferred with the same scope argument.
defer() {
    scope=auto
    case "${1:-}" in
        --scope=*)
            scope=$(echo "$1" | cut -d = -f 2)
            shift
            ;;
    esac
    (
        flock -n 9 || exit 1
        if [ -e ".${scope}_defer_count" ]; then
            count="$(cat ".${scope}_defer_count")"
            count="$((count + 1))"
        else
            count=0
        fi
        (
            echo "#!/bin/sh -ex"
            echo "$@"
        ) > ".${scope}_defer_${count}"
        chmod +x ".${scope}_defer_${count}"
        echo  "$count" > ".${scope}_defer_count"
    ) 9>.defer_lock
}

# run_deferred runs deferred commands
#
# synopsis: run_deferred [--scope=SCOPE]
#
# Deferred commands are executed in LIFO order. The scope determines the group
# of deferred commands to execute. This allows to have separate groups for the
# prepare/restore and for the execute sections.
run_deferred() {
    printf "\nRUNNING DEFERRED COMMANDS\n\n"
    scope=auto
    case "${1:-}" in
        --scope=*)
            scope=$(echo "$1" | cut -d = -f 2)
            shift
            ;;
    esac
    (
        flock -n 9 || exit 1
        if [ ! -e ".${scope}_defer_count" ]; then
            return
        fi
        count="$(cat ".${scope}_defer_count")"
        for i in $(seq "$count" -1 0); do
            echo "DEFER-${i}"
            "./.${scope}_defer_${i}"
            rm "./.${scope}_defer_${i}"
        done
        rm ".${scope}_defer_count"
    ) 9>.defer_lock
}
