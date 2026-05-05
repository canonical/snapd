#!/usr/bin/sh

# A simple deny once test of a write prompt.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Attempt to write the file (should fail)"
snap run --shell prompt-requester.home -c "echo it is written > ${TEST_DIR}/test.txt" || true

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

if [ -f "${TEST_DIR}/test.txt" ] ; then
	echo "file creation unexpectedly succeeded for test.txt"
	exit 1
fi
