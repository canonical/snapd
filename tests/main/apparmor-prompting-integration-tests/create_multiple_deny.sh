#!/usr/bin/sh

# A test that replying with deny always denies multiple files which match the
# path pattern to be created, but doesn't deny other file creation.

TEST_DIR="$1"

for name in test1.txt test2.md succeed.txt test3.pdf ; do
	echo "Attempt to write $name"
	snap run --shell prompting-client.scripted -c "echo $name is written > ${TEST_DIR}/${name}"
done

# Wait for the client to write its result and exit
timeout 5 sh -c 'while pgrep -f "prompting-client-scripted" > /dev/null; do sleep 0.1; done'

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

for name in test1.txt test2.md test3.pdf ; do
	if [ -f "${TEST_DIR}/${name}" ] ; then
		echo "file creation unexpectedly succeeded"
		exit 1
	fi
done

TEST_OUTPUT="$(cat "${TEST_DIR}/succeed.txt")"
if [ "$TEST_OUTPUT" != "succeed.txt is written" ] ; then
	echo "file creation failed"
	exit 1
fi
