#!/bin/sh -x
# When "set -e" is in effect it is natural to expect that failing commands
# cause execution of the script to fail with an error code. This logic is
# obviously not applied to all the possible cases as if-then-else expressions
# must be allowed to fail to execute correctly.
#
# We've learned that negating a shell command is treated like an if-then-else
# expression, in that it doesn't cause the script to fail to execute.
set -e
not true
echo "not reached"
