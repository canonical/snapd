#!/usr/bin/sh

# A simulation of what often happens when one attempts to download a file from
# a web browser: read access on the parent directory, then write access to
# the particular file in that directory.

TEST_DIR="$1"

echo "Run the prompting client in scripted mode in the background"
prompting-client.scripted \
	--script="${TEST_DIR}/script.json" \
	--grace-period=1 \
	--var="BASE_PATH:${TEST_DIR}" | tee "${TEST_DIR}/result" &

sleep 1 # give the client a chance to start listening

# Prep a "Downloads" directory with an existing file in it
mkdir -p "${TEST_DIR}/Downloads"
touch "${TEST_DIR}/Downloads/existing.txt"

echo "Attempt to list the contents of the downloads directory"
if ! snap run --shell prompting-client.scripted -c "ls ${TEST_DIR}/Downloads" | grep "existing.txt" ; then
	echo "Failed to list contents of ${TEST_DIR}/Downloads"
	exit 1
fi

echo "Attempt to write the file"
snap run --shell prompting-client.scripted -c "echo it is written > ${TEST_DIR}/Downloads/test.txt"

echo "Attempt to chmod the file after it has been written"
snap run --shell prompting-client.scripted -c "chmod 664 ${TEST_DIR}/Downloads/test.txt"

sleep 5 # give the client a chance to write its result and exit

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

# We don't expect success, since there should be a rule conflict with the rule
# we just added to grant read access forever
if [ "$CLIENT_OUTPUT" = "success" ] ; then
	echo "test unexpectedly succeeded, expected rule conflict error"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if ! grep 'cannot add rule: a rule with conflicting path pattern and permission already exists in the rule database' "${TEST_DIR}/result" ; then
	echo "test failed, expected rule conflict error"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ -f "$TEST_DIR/test.txt" ] ; then
	echo "write unexpectedly succeeded"
	cat "${TEST_DIR}/test.txt"
	exit 1
fi
