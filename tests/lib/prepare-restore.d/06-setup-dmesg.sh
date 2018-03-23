#!/bin/sh
# Clear the kernel ring buffer before every test.
#
# Some tests use dmesg to look for certain kind of issues. Clearing the
# buffer here allows us to ensure that only messages logged since the
# beginning of the current tests are observed.

on_prepare_project_each() {
    dmesg -c > /dev/null
}
