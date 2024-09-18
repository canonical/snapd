#!/usr/bin/sh

# A test that replying with allow always allows multiple files which match the
# path pattern to be created, but doesn't allow other file creation.

TEST_DIR="$1"

echo "Run the prompting client in scripted mode in the background"
prompting-client.scripted \
	--script="${TEST_DIR}/script.json" \
	--var="BASE_PATH:${TEST_DIR}" | tee "${TEST_DIR}/result" &

sleep 1 # give the client a chance to start listening

for name in test1.txt test2.md fail.txt test3.pdf ; do
	echo "Attempt to write $name"
	$SNAP_DO "$name is written > ${TEST_DIR}/${name}"
done

sleep 1 # give the client a chance to write its result and exit

CLIENT_RESULT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_RESULT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

for name in test1.txt test2.md test3.pdf ; do
	TEST_OUTPUT="$(cat "${TEST_DIR}/${name}")"
	if [ "$TEST_OUTPUT" != "$name is written" ] ; then
		echo "file creation failed"
		exit 1
	fi
done

if [ -f "${TEST_DIR}/fail.txt" ] ; then
	echo "file creation unexpectedly succeeded"
	exit 1
fi
