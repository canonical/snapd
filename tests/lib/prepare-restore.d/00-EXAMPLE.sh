#!/bin/sh

# This directory can contain any number of files that assist in the
# prepare-restore process.  Each file should be named NN-purpose.sh where NN is
# a two-digit priority code. Lower priority codes have an opportunity to run
# first.
#
# Each file is a shell script that is sourced in a private shell. The script can define
# any number of functions named on_{prepare,restore}_{project,suite}{,_each}. All such
# functions are optional.
#
# If a function is present it will be called at the right time, as if the same
# shell code was present in the appropriate stanza in spread.yaml.
#
# The main idea is that this design increases modularity and helps to make the
# code manageable as code responsible for a given task, that spans many phases,
# can be placed next to each other in one file, and not have to be scattered in
# one huge file somewhere.

# == Project prepare/restore ==

on_prepare_project() {
    # This phase runs once, when the whole project is prepared.
    echo "PHASE: prepare project"
}

on_restore_project() {
    # This phase runs once, when the whole project is restored.
    echo "PHASE: restore project"
}

# == Project wide task prepare/restore ==

on_prepare_project_each() {
    # This phase runs once for each task but is affecting the whole
    # project, not just individual suite or task.
    echo "PHASE: prepare project each"
}

on_restore_project_each() {
    # This phase runs once for each task but is affecting the whole
    # project, not just individual suite or task.
    echo "PHASE: restore project each"
}

# == Suite level prepare/restore ==

on_prepare_suite() {
    # This phase runs when a specific suite is prepared
    echo "PHASE: prepare suite"
}

on_restore_suite() {
    # This phase runs when a specific suite is restored
    echo "PHASE: restore suite"
}

# == Task level prepare/restore ==

on_prepare_suite_each() {
    # This phase runs when each task in a given suite is prepared.
    echo "PHASE: prepare suite each"
}

on_restore_suite_each() {
    # This phase runs when each task in a given suite is restored.
    echo "PHASE: restore suite each"
}

