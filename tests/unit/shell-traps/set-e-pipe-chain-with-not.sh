#!/bin/sh -x
# When "set -e" is in effect it is natural to expect that failing commands
# cause execution of the script to fail with an error code. This logic is
# obviously not applied to all the possible cases as if-then-else expressions
# must be allowed to fail to execute correctly.
#
# We've learned that negating a shell command is treated like an if-then-else
# expression, in that it doesn't cause the script to fail to execute.
#
# When applied to pipe expressions the result of the last element of the pipe
# determines the result of the expression and the use of "not" instead of "!"
# makes the non-zero result fatal.
set -e
echo foo bar | not grep "foo"
echo "not reached"
