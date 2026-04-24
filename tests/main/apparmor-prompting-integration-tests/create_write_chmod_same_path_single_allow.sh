#!/usr/bin/sh

# A test of lifespan "single" file descriptor-level caching, where we expect
# multiple operations on the same file descriptor which all map to the abstract
# "write" permission to be handled by a single allow once.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Create, write, and chmod the file"
snap run --shell prompting-client.scripted -c "touch ${TEST_DIR}/test.txt"
snap run --shell prompting-client.scripted -c "echo some-content > ${TEST_DIR}/test.txt"
snap run --shell prompting-client.scripted -c "chmod 654 ${TEST_DIR}/test.txt"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if ! [ -f "${TEST_DIR}/test.txt" ] ; then
	echo "create failed for test.txt"
	exit 1
fi
TEST_OUTPUT="$(cat "${TEST_DIR}/test.txt")"
if [ "$TEST_OUTPUT" != "some-content" ] ; then
	echo "write failed for test.txt"
	exit 1
fi
PERMS="$(stat --format=%a "${TEST_DIR}/test.txt")"
if [ "$PERMS" != "654" ] ; then
	echo "chmod failed for test.txt"
	exit 1
fi
