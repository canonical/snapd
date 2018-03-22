#!/bin/sh
# In case someone tries to run the test suite in a container we want to fail
# early with a helpful error message. Unfortunately our test suite doesn't work
# in a container.

on_prepare_project() {
    if systemd-detect-virt -c; then
        echo "Tests cannot run inside a container"
        exit 1
    fi
}
