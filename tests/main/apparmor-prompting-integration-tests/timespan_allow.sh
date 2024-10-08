#!/usr/bin/sh

# A test that replying with allow always allows multiple files which match the
# path pattern to be created, but doesn't allow other file creation.

TEST_DIR="$1"

for name in test1.txt test2.txt test3.txt ; do
	echo "Attempt to write $name"
	snap run --shell prompting-client.scripted -c "echo $name is written > ${TEST_DIR}/${name}"
done

sleep 1 # wait for the rule to time out

echo "Attempt to write test4.txt (should fail)"
snap run --shell prompting-client.scripted -c "echo test4.txt is written > ${TEST_DIR}/test4.txt"

# Wait for the client to write its result and exit
timeout 5 sh -c 'while pgrep -f "prompting-client-scripted" > /dev/null; do sleep 0.1; done'

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

for name in test1.txt test2.txt test3.txt ; do
	TEST_OUTPUT="$(cat "${TEST_DIR}/${name}")"
	if [ "$TEST_OUTPUT" != "$name is written" ] ; then
		echo "file creation failed"
		exit 1
	fi
done

if [ -f "${TEST_DIR}/test4.txt" ] ; then
	echo "file creation unexpectedly succeeded"
	exit 1
fi
