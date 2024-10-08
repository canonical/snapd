#!/usr/bin/sh

# A simple deny once test of a write prompt.

TEST_DIR="$1"

echo "Attempt to write the file (should fail)"
snap run --shell prompting-client.scripted -c "echo it is written > ${TEST_DIR}/test.txt"

# Wait for the client to write its result and exit
timeout 5 sh -c 'while pgrep -f "prompting-client-scripted" > /dev/null; do sleep 0.1; done'

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ -f "${TEST_DIR}/test.txt" ] ; then
	echo "test script unexpectedly succeeded"
	exit 1
fi
