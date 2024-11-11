#!/usr/bin/sh

# A test that replying with allow once does not action previous matching creates.
#
# When creating a new file is blocked on a reply to a request prompt, the
# directory in which the file will be created is locked from other writes.
# Thus, we can't queue up multiple outstanding file creations in the same
# directory. Instead, we must create files in different directories in order
# for this test to succeed. Reads and writes to already-existing files in a
# directory are not blocked by file creations pending replies in that same
# directory.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

WRITABLE="$(snap run --shell prompting-client.scripted -c 'cd ~; pwd')/$(basename "$TEST_DIR")"
snap run --shell prompting-client.scripted -c "mkdir -p $WRITABLE"

for dir in test1 test2 test3 ; do
	mkdir -p "${TEST_DIR}/${dir}"
	name="${dir}/file.txt"
	echo "Attempt to create $name in the background"
	snap run --shell prompting-client.scripted -c "touch ${WRITABLE}/${dir}-started; echo $name is written > ${TEST_DIR}/${name}; touch ${WRITABLE}/${dir}-finished" &
	if ! timeout "$TIMEOUT" sh -c "while ! [ -f '${WRITABLE}/${dir}-started' ] ; do sleep 0.1 ; done" ; then
		echo "failed to start create of $name within timeout period"
		exit 1
	fi
done

for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	echo "Check that create for $name has not yet finished"
	if [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "create of $name finished before create for test4/file.txt started"
		exit 1
	fi
done

echo "Attempt to create test4/file.txt (for which client will reply allow single)"
mkdir -p "${TEST_DIR}/test4"
snap run --shell prompting-client.scripted -c "echo test4/file.txt is written > ${TEST_DIR}/test4/file.txt"

for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	echo "Check that create for $name was not actioned by reply for test4/file.txt"
	# NOTE: if one checks [ -f "${TEST_DIR}/${name}" ], it may kill the blocked create
	if [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "create of $name finished before create for test5/file.txt started"
		exit 1
	fi
done

echo "Attempt to create test5/file.txt (for which client will reply deny forever)"
mkdir -p "${TEST_DIR}/test5"
snap run --shell prompting-client.scripted -c "echo test5/file.txt is written > ${TEST_DIR}/test5/file.txt"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

for dir in test1 test2 test3 ; do
	name="${dir}/file.txt"
	echo "Check that create for $name has finished"
	if ! [ -f "${WRITABLE}/${dir}-finished" ] ; then
		echo "create of $name did not finish after client replied"
		exit 1
	fi
done

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

TEST_OUTPUT="$(cat "${TEST_DIR}/test4/file.txt")"
if [ "$TEST_OUTPUT" != "test4/file.txt is written" ] ; then
	echo "file creation failed for test4/file.txt"
	exit 1
fi

for dir in test1 test2 test3 test5; do
	name="${dir}/file.txt"
	if [ -f "${TEST_DIR}/${name}" ] ; then
		echo "file creation unexpectedly succeeded for $name"
		exit 1
	fi
done
