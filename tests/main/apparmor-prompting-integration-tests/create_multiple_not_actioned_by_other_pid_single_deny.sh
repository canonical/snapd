#!/usr/bin/sh

# A test that replying with deny once does not action previous matching prompts.
#
# When creating a new file is blocked on a reply to a request prompt, the
# directory in which the file will be created is locked from other writes.
# Thus, we can't queue up multiple outstanding file creations in the same
# directory. Instead, we must create files in different directories in order
# for this test to succeed. Reads and writes to already-existing files in a
# directory are not blocked by file creations pending replies in that same
# directory.

TEST_DIR="$1"

WRITABLE="$(snap run --shell prompting-client.scripted -c 'cd ~; pwd')/$(basename "$TEST_DIR")"
snap run --shell prompting-client.scripted -c "mkdir -p $WRITABLE"

for dir in test1 test2 test3 ; do
	mkdir -p "${TEST_DIR}/${dir}"
	name="${dir}/file.txt"
	echo "Attempt to write $name in the background"
	snap run --shell prompting-client.scripted -c "touch ${WRITABLE}/${dir}-started; echo $name is written > ${TEST_DIR}/${name}; touch ${WRITABLE}/${dir}-finished" &
	if ! timeout 10 sh -c "while ! [ -f '${WRITABLE}/${dir}-started' ] ; do sleep 0.1 ; done" ; then
		echo "failed to start write of $name within timeout period"
		exit 1
	fi
done

for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	echo "Check that write for $name has not yet finished"
	if [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "write of $name finished before write for test4/file.txt started"
		exit 1
	fi
done

echo "Attempt to write test4/file.txt (for which client will reply deny single)"
mkdir -p "${TEST_DIR}/test4"
snap run --shell prompting-client.scripted -c "echo test4/file.txt is written > ${TEST_DIR}/test4/file.txt"

echo "Check that original files have not yet been written"
for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	if [ -f "${TEST_DIR}/${name}" ] ; then
		echo "file creation unexpectedly succeeded early for $name"
		exit 1
	fi
	echo "Check that write for $name was not actioned by reply for test4/file.txt"
	if [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "write of $name finished before write for test5/file.txt started"
		exit 1
	fi
done

echo "Attempt to write test5/file.txt (for which client will reply allow forever)"
mkdir -p "${TEST_DIR}/test5"
snap run --shell prompting-client.scripted -c "echo test5/file.txt is written > ${TEST_DIR}/test5/file.txt"

# Wait for the client to write its result and exit
timeout 5 sh -c 'while pgrep -f "prompting-client-scripted" > /dev/null; do sleep 0.1; done'

for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	echo "Check that write for $name has finished"
	if ! [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "write of $name did not finish after client replied"
		exit 1
	fi
done

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ -f "${TEST_DIR}/test4/file.txt" ] ; then
	echo "file creation unexpectedly succeeded for test4/file.txt"
	exit 1
fi

for dir in test1 test2 test3 test5; do
	name="${dir}/file.txt"
	TEST_OUTPUT="$(cat "${TEST_DIR}/${name}")"
	if [ "$TEST_OUTPUT" != "$name is written" ] ; then
		echo "file creation failed for $name"
		exit 1
	fi
done
