#!/usr/bin/sh

# A simple allow once test of a read prompt.

TEST_DIR="$1"

echo "Run the prompting client in scripted mode in the background"
prompting-client.scripted \
	--script="${TEST_DIR}/script.json" \
	--grace-period=1 \
	--var="BASE_PATH:${TEST_DIR}" | tee "${TEST_DIR}/result" &

sleep 1 # give the client a chance to start listening

echo "Prepare the file to be read"
echo "testing testing 1 2 3" | tee "${TEST_DIR}/test.txt"

echo "Attempt to read the file"
TEST_OUTPUT="$(snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/test.txt")"

sleep 5 # give the client a chance to write its result and exit

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
