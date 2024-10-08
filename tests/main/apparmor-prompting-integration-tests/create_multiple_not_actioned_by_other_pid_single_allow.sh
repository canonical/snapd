#!/usr/bin/sh

# A test that replying with allow forever actions previous matching prompts.

TEST_DIR="$1"

WRITABLE="$(snap run --shell prompting-client.scripted -c "cd ~; pwd")/$(basename "$TEST_DIR")"
snap run --shell prompting-client.scripted -c "mkdir -p $WRITABLE"

for name in test1.txt test2.txt test3.txt ; do
	echo "Attempt to write $name in the background"
	snap run --shell prompting-client.scripted -c "echo started > ${WRITABLE}/${name}; echo $name is written > ${TEST_DIR}/${name}" &
	timeout 10 sh -c "while ! [ -f '${WRITABLE}/${name}' ] ; do sleep 0.1 ; done"
done

echo "Attempt to write test4.txt (for which client will reply allow single)"
snap run --shell prompting-client.scripted -c "echo test4.txt is written > ${TEST_DIR}/test4.txt"

echo "Attempt to write test5.txt (for which client will reply deny forever)"
snap run --shell prompting-client.scripted -c "echo test5.txt is written > ${TEST_DIR}/test5.txt"

# Wait for the client to write its result and exit
timeout 5 sh -c 'while pgrep -f "prompting-client-scripted" > /dev/null; do sleep 0.1; done'

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

TEST_OUTPUT="$(cat "${TEST_DIR}/test4.txt")"
if [ "$TEST_OUTPUT" != "test4.txt is written" ] ; then
	echo "file creation failed for test4.txt"
	exit 1
fi

for name in test1.txt test2.txt test3.txt test5.txt; do
	if [ -f "${TEST_DIR}/${name}" ] ; then
		echo "file creation unexpectedly succeeded for $name"
		exit 1
	fi
done
