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

echo "Attempt to list the contents of the parent directory"
snap run --shell prompting-client.scripted -c "ls ${TEST_DIR}/Downloads"

echo "Attempt to write the file"
snap run --shell prompting-client.scripted -c "echo it is written > ${TEST_DIR}/Downloads/test.txt"

echo "Attempt to chmod the file after it has been written"
snap run --shell prompting-client.scripted -c "chmod 664 ${TEST_DIR}/Downloads/test.txt"

sleep 5 # give the client a chance to write its result and exit

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

TEST_OUTPUT="$(cat "${TEST_DIR}/Downloads/test.txt")"

if [ "$TEST_OUTPUT" != "it is written" ] ; then
	echo "test script failed"
	exit 1
fi
