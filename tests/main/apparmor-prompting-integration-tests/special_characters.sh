#!/usr/bin/sh

# Test prompting for filepaths which contain special characters.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

FIRST_CONTENT="a file with square brackets and unicode"
SECOND_CONTENT="a file with all the special characters"

# Prompt sequence prompt-filters are regular expressions, and they're stored as
# json, so we annoyingly have to escape special characters twice in just the
# prompt-filter path, with extra complexity for literal '\' characters.
echo "Prepare the files to be read"
echo "$FIRST_CONTENT" | tee "${TEST_DIR}/[アニメ][ゲーム動画].mkv"
echo "$SECOND_CONTENT" | tee "${TEST_DIR}/foo*?()[]{}\\"

echo "Attempt to read the first file"
FIRST_OUTPUT="$(snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/'[アニメ][ゲーム動画].mkv'")"

echo "Skip reading the second file as there's an issue with the prompting-client.scripted parsing the sequence"
# TODO: actually do the second read
#echo "Attempt to read the second file"
#SECOND_OUTPUT="$(snap run --shell prompting-client.scripted -c "cat ${TEST_DIR}/'foo*?()[]{}\\'")"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if [ "$FIRST_OUTPUT" != "$FIRST_CONTENT" ] ; then
	echo "test script failed"
	exit 1
fi

# TODO: actually check the second output
#if [ "$SECOND_OUTPUT" != "$SECOND_CONTENT" ] ; then
#	echo "test script failed"
#	exit 1
#fi
