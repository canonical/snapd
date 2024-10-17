#!/usr/bin/sh

# A test that multiple writes and reads in the same directory can be queued up,
# replies with lifespan forever action previous prompts, replying with broader
# permissions requested handles future requests for other permissions, and the
# rule with the most specific path pattern has precedence.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

WRITABLE="$(snap run --shell prompting-client.scripted -c 'cd ~; pwd')/$(basename "$TEST_DIR")"
snap run --shell prompting-client.scripted -c "mkdir -p $WRITABLE"

# First, queue up writes

for name in test1.txt test2.txt test3.txt ; do
	echo "Attempt to write $name in the background"
	echo "not written" > "${TEST_DIR}/${name}"
	snap run --shell prompting-client.scripted -c "touch ${WRITABLE}/${name}-write-started; echo $name is written > ${TEST_DIR}/${name}; touch ${WRITABLE}/${name}-write-finished" &
	if ! timeout "$TIMEOUT" sh -c "while ! [ -f '${WRITABLE}/${name}-write-started' ] ; do sleep 0.1 ; done" ; then
		echo "failed to start write of $name within timeout period"
		exit 1
	fi
done

for name in test1.txt test2.txt test3.txt ; do
	echo "Check that write for $name has not yet finished"
	if [ -f "${WRITABLE}/${name}-write-finished" ] ; then
		echo "write of $name finished before write for test4.txt started"
		exit 1
	fi
done

echo "Attempt to write test4.txt (for which client will reply)"
echo "not written" > "${TEST_DIR}/test4.txt"
snap run --shell prompting-client.scripted -c "echo test4.txt is written > ${TEST_DIR}/test4.txt"

# Reply for test4.txt will allow always write test*.txt

for name in test1.txt test2.txt test3.txt ; do
	echo "Check that write for $name has finished"
	if ! timeout "$TIMEOUT" sh -c "while ! [ -f '${WRITABLE}/${name}-write-finished' ] ; do sleep 0.1 ; done" ; then
		echo "write of $name did not finish after client replied"
		exit 1
	fi
done

for name in test1.txt test2.txt test3.txt test4.txt ; do
	TEST_OUTPUT="$(cat "${TEST_DIR}/${name}")"
	if [ "$TEST_OUTPUT" != "$name is written" ] ; then
		echo "write failed for $name"
		exit 1
	fi
done

# Next queue up reads

for name in test1.txt test2.txt test3.txt ; do
	echo "Attempt to read $name in the background"
	snap run --shell prompting-client.scripted -c "touch ${WRITABLE}/${name}-read-started; cat ${TEST_DIR}/${name} > ${WRITABLE}/${name}; touch ${WRITABLE}/${name}-read-finished" &
	if ! timeout "$TIMEOUT" sh -c "while ! [ -f '${WRITABLE}/${name}-read-started' ] ; do sleep 0.1 ; done" ; then
		echo "failed to start read of $name within timeout period"
		exit 1
	fi
done

for name in test1.txt test2.txt test3.txt ; do
	echo "Check that read for $name has not yet finished"
	if [ -f "${WRITABLE}/${name}-read-finished" ] ; then
		echo "read of $name finished before read for test4/.txt started"
		exit 1
	fi
done

echo "Attempt to read test4.txt (for which client will reply)"
snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/test4.txt > ${WRITABLE}/test4.txt"

# Reply for test4.txt will deny always read|write test*

for name in test1.txt test2.txt test3.txt ; do
	echo "Check that read for $name has finished"
	if ! timeout "$TIMEOUT" sh -c "while ! [ -f '${WRITABLE}/${name}-read-finished' ] ; do sleep 0.1 ; done" ; then
		echo "read of $name did not finish after client replied"
		exit 1
	fi
done

for name in test1.txt test2.txt test3.txt test4.txt ; do
	TEST_OUTPUT="$(cat "${WRITABLE}/${name}")"
	if [ "$TEST_OUTPUT" = "$name is written" ] ; then
		echo "read unexpectedly succeeded for $name"
		exit 1
	fi
done

# Now check the following:
# create test5.txt -> no prompt, allowed
# create test5.md -> no prompt, denied
# read test5.txt -> no prompt, denied
# read test5.md -> no prompt, denied
# create other.txt -> prompt, reply with deny (mostly to make sure the client lives long enough)

echo "Attempt to create test5.txt (should be allowed by original rule)"
snap run --shell prompting-client.scripted -c "echo test5.txt is written > ${TEST_DIR}/test5.txt"
TEST_OUTPUT="$(cat "${TEST_DIR}/test5.txt")"
if [ "$TEST_OUTPUT" != "test5.txt is written" ] ; then
	echo "file creation failed for test5.txt"
	exit 1
fi

echo "Attempt to create test5.md (should be denied by previous rule)"
snap run --shell prompting-client.scripted -c "echo test5.md is written > ${TEST_DIR}/test5.md"
if [ -f "${TEST_DIR}/test5.md" ] ; then
	echo "file creation unexpectedly succeeded for test5.md"
	exit 1
fi

for name in test5.txt test5.md ; do
	echo "Attempt to read $name (should be denied by previous rule)"
	echo "$name is written" > "${TEST_DIR}/${name}"
	snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/${name} > ${WRITABLE}/${name}"
	TEST_OUTPUT="$(cat "${WRITABLE}/${name}")"
	if [ "$TEST_OUTPUT" = "$name is written" ] ; then
		echo "read unexpectedly succeeded for $name"
		exit 1
	fi
done

echo "Attempt to create other.txt (should trigger prompt, which is then denied)"
snap run --shell prompting-client.scripted -c "echo other.txt is written > ${TEST_DIR}/other.txt"
if [ -f "${TEST_DIR}/other.txt" ] ; then
	echo "file creation unexpectedly succeeded for other.txt"
	exit 1
fi

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi
