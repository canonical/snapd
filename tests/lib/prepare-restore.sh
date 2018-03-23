#!/bin/bash
set -x
# NOTE: We must set -e so that any failures coming out of the various
# statements we execute stops the build. The code is not (yet) written to
# handle errors in general.
set -e
# Set pipefail option so that "foo | bar" behaves with fewer surprises by
# failing if foo fails, not just if bar fails.
set -o pipefail

# Run a set of scripts in the prepare-restore.d directory. From each
# script run, if present, the function on_$phase, where phase is one of
# {prepare,restore}_{project,suite}{,_each}.
do_phase() {
    phase="$1" && shift
    for module in "$TESTSLIB"/prepare-restore.d/*.sh; do
        (
            set -u
            set -e
            # shellcheck disable=SC1090
            . "$module"
            if [ "$(type -t "on_$phase")" = "function" ]; then
                echo "running phase $phase for module $module" >> /tmp/prepare-restore.log
                "on_$phase"
            fi
        )
    done
}

prepare_project() {
    # Set REUSE_PROJECT to reuse the previous prepare when also reusing the server.
    [ "$REUSE_PROJECT" != 1 ] || exit 0

    # check that we are not updating
    if [ "$(bootenv snap_mode)" = "try" ]; then
        echo "Ongoing reboot upgrade process, please try again when finished"
        exit 1
    fi

    # This is here because it does "exit 0" and the phased scripts cannot do that.
    if [ "$SPREAD_BACKEND" = external ]; then
        chown test.test -R "$PROJECT_PATH"
        exit 0
    fi

    echo "Running with SNAP_REEXEC: $SNAP_REEXEC"

    do_phase prepare_project
}

case "$1" in
    --prepare-project)
        do_phase prepare_project
        ;;
    --prepare-project-each)
        do_phase prepare_project_each
        ;;
    --prepare-suite)
        do_phase prepare_suite
        ;;
    --prepare-suite-each)
        do_phase prepare_suite_each
        ;;
    --restore-suite-each)
        do_phase restore_suite_each
        ;;
    --restore-suite)
        do_phase restore_suite
        ;;
    --restore-project-each)
        do_phase restore_project_each
        ;;
    --restore-project)
        do_phase restore_project
        ;;
    *)
        echo "unsupported argument: $1"
        echo "try one of --{prepare,restore}-{project,suite}{,-each}"
        exit 1
        ;;
esac
