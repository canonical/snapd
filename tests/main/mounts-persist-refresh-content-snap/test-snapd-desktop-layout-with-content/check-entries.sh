#!/bin/bash

# This script checks that content and layout are mounted at the expected paths;
# we also check the contents of the files, to detect buggy situations where
# only the mount point would exist.

set -ex

test "$(< /bin/program/file-a-1)" = "file-a-1"
test "$(< /lib/libfootest.so)" = "file-a-2"
test -d /usr/share/dir-1
test "$(echo /usr/share/dir-1/*)" = "/usr/share/dir-1/file-a-1 /usr/share/dir-1/file-b-1"
test "$(< /usr/share/file-2)" = "file-b-2"
