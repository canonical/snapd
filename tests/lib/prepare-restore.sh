#!/bin/bash

prepare_project() {
    true
}

prepare_project_each() {
    true
}

restore_project_each() {
    true
}

restore_project() {
    true
}

case "$1" in
    --prepare-project)
        prepare_project
        ;;
    --prepare-project-each)
        prepare_project_each
        ;;
    --restore-project-each)
        restore_project_each
        ;;
    --restore-project)
        restore_project
        ;;
    *)
        echo "unsupported argument: $1"
        echo "try one of --{prepare,restore}-project{,-each}"
        exit 1
        ;;
esac
