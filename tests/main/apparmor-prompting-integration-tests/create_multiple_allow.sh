#!/usr/bin/sh

# A test that replying with allow always allows multiple files which match the
# path pattern to be created, but doesn't allow other file creation.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

for name in test1.txt test2.md fail.txt test3.pdf ; do
	echo "Attempt to write $name"
	snap run --shell prompting-client.scripted -c "echo $name is written > ${TEST_DIR}/${name}"
done

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

for name in test1.txt test2.md test3.pdf ; do
	TEST_OUTPUT="$(cat "${TEST_DIR}/${name}")"
	if [ "$TEST_OUTPUT" != "$name is written" ] ; then
		echo "file creation failed for $name"
		exit 1
	fi
done

if [ -f "${TEST_DIR}/fail.txt" ] ; then
	echo "file creation unexpectedly succeeded for fail.txt"
	exit 1
fi
