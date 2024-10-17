#!/usr/bin/sh

# A simple allow once test of a read prompt.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Prepare the file to be read"
echo "testing testing 1 2 3" | tee "${TEST_DIR}/test.txt"

echo "Attempt to read the file"
TEST_OUTPUT="$(snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/test.txt")"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ "$TEST_OUTPUT" != "testing testing 1 2 3" ] ; then
	echo "test script failed"
	exit 1
fi
