#!/usr/bin/sh

# A simulation of what often happens when one attempts to download a file from
# a web browser: read access on the parent directory, then write access to
# the particular file in that directory.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

# Prep a "Downloads" directory with an existing file in it
mkdir -p "${TEST_DIR}/Downloads"
touch "${TEST_DIR}/Downloads/existing.txt"

echo "Attempt to list the contents of the downloads directory"
if ! snap run --shell prompt-requester.home -c "ls ${TEST_DIR}/Downloads" | grep "existing.txt" ; then
	echo "Failed to list contents of ${TEST_DIR}/Downloads"
	exit 1
fi

echo "Attempt to write the file"
snap run --shell prompt-requester.home -c "echo it is written > ${TEST_DIR}/Downloads/test.txt"

echo "Attempt to chmod the file after it has been written"
snap run --shell prompt-requester.home -c "chmod 664 ${TEST_DIR}/Downloads/test.txt"

# Wait for the client to write its result and exit
for i in $(seq "$TIMEOUT") ; do
	if ! pgrep -af "prompting-client.scripted.*${TEST_DIR}" ; then
		break
	fi
	sleep 1
done
if pgrep -af "prompting-client.scripted.*${TEST_DIR}" ; then
	echo "prompting-client.scripted still running"
	exit 1
fi

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

# Rules with identical path patterns are merged, so we don't expect any rules
# with duplicate path patterns.
snap debug api /v2/interfaces/requests/rules | jq '."result".[]."constraints"."path-pattern"' | grep "${TEST_DIR}" | uniq -c | grep '^[[:space:]]*1[[:space:]]'
! snap debug api /v2/interfaces/requests/rules | jq '."result".[]."constraints"."path-pattern"' | grep "${TEST_DIR}" | uniq -c | grep -q '^[[:space:]]*[^1[[:space:]]]'

TEST_OUTPUT="$(cat "${TEST_DIR}/Downloads/test.txt")"

if [ "$TEST_OUTPUT" != "it is written" ] ; then
	echo "write failed for test.txt"
	exit 1
fi
