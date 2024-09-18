#!/usr/bin/sh

# A simple deny once test of a read prompt.

TEST_DIR="$1"

echo "Run the prompting client in scripted mode in the background"
prompting-client.scripted \
	--script="${TEST_DIR}/script.json" \
	--var="BASE_PATH:${TEST_DIR}" | tee "${TEST_DIR}/result" &

sleep 1 # give the client a chance to start listening

echo "Prepare the file to be read"
echo "testing testing 1 2 3" | tee "${TEST_DIR}/test.txt"

echo "Attempt to read the file (should fail)"
TEST_OUTPUT="$($SNAP_DO cat "${TEST_DIR}/test.txt")"

ECODE=$?

sleep 1 # give the client a chance to write its result and exit

CLIENT_RESULT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_RESULT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ "$TEST_OUTPUT" = "testing testing 1 2 3" ] ; then
	echo "test script unexpectedly succeeded"
	exit 1
fi
