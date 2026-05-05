#!/usr/bin/sh

# A simple deny once test of a read prompt.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Prepare the file to be read"
echo "testing testing 1 2 3" | tee "${TEST_DIR}/test.txt"

echo "Attempt to read the file (should fail)"
TEST_OUTPUT="$(snap run --shell prompt-requester.home -c "cat ${TEST_DIR}/test.txt" || true)"

# Wait for the client to write its result and exit
for i in $(seq "$TIMEOUT") ; do
	if ! pgrep -af "prompting-client.scripted.*${TEST_DIR}" ; then
		break
	fi
	sleep 1
done
if pgrep -af "prompting-client.scripted.*${TEST_DIR}" ; then
	echo "prompting-client.scripted still running"
	exit 1
fi

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ "$TEST_OUTPUT" = "testing testing 1 2 3" ] ; then
	echo "test script unexpectedly succeeded"
	exit 1
fi
