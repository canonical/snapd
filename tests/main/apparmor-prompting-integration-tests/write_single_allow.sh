#!/usr/bin/sh

# A simple allow once test of a write prompt.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Attempt to write the file"
snap run --shell prompting-client.scripted -c "echo it is written > ${TEST_DIR}/test.txt"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

TEST_OUTPUT="$(cat "${TEST_DIR}/test.txt")"

if [ "$TEST_OUTPUT" != "it is written" ] ; then
	echo "write failed for test.txt"
	exit 1
fi
