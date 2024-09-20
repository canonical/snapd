#!/usr/bin/sh

# A simple deny once test of a write prompt.

TEST_DIR="$1"

echo "Run the prompting client in scripted mode in the background"
prompting-client.scripted \
	--script="${TEST_DIR}/script.json" \
	--grace-period=1 \
	--var="BASE_PATH:${TEST_DIR}" | tee "${TEST_DIR}/result" &

sleep 1 # give the client a chance to start listening

echo "Attempt to write the file (should fail)"
snap run --shell prompting-client.scripted -c "echo it is written > ${TEST_DIR}/test.txt"

sleep 5 # give the client a chance to write its result and exit

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
