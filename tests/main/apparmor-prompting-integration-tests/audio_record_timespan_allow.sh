#!/usr/bin/sh

# A test of allow with timespan for the audio-record interface.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

SNAP_HOME="$HOME/snap/prompt-requester/current"
rm -f "$SNAP_HOME/running.pid" # Clean up any PID file from previous run

# The audio-record interface doesn't use a target file to trigger a prompt.
# Instead, we use a file to tell the snap to finish running. We need a snap
# running under a cgroup in order for the "ask" request to be handled correctly.
TARGET_FILE="$SNAP_HOME/finish"
rm -f "$TARGET_FILE"

# Start the snap running in the background so "ask" can use its PID to look up
# its cgroup, and from that derive the snap name.
prompt-requester.wait-for "$TARGET_FILE" &
WAITER_SHELL_PID="$!"

# Background PID will be of the /snap/bin command, not the internal script, so
# need to get the real PID. The snap will write its own PID to
# $SNAP_HOME/running.pid
for i in $(seq 10) ; do
	if [ -f "$SNAP_HOME/running.pid" ] ; then
		break
	fi
	sleep 1
done
test -f "$SNAP_HOME/running.pid"
WAITER_SNAP_PID="$(cat "$SNAP_HOME/running.pid")"

# Actually trigger the request by querying the API
ASK_BODY="{\"action\": \"ask\", \"interface\": \"audio-record\", \"pid\": $WAITER_SNAP_PID}"
RESULT="$(echo "$ASK_BODY" | snap debug api -X POST -H 'Content-Type: application/json' "/v2/interfaces/requests")"
echo "$RESULT" | grep '"status-code": 200'
echo "$RESULT" | grep '"outcome": "allow"'

# Trigger a second request
ASK_BODY="{\"action\": \"ask\", \"interface\": \"audio-record\", \"pid\": $WAITER_SNAP_PID}"
RESULT="$(echo "$ASK_BODY" | snap debug api -X POST -H 'Content-Type: application/json' "/v2/interfaces/requests")"
echo "$RESULT" | grep '"status-code": 200'
echo "$RESULT" | grep '"outcome": "allow"'

# Trigger a third request
ASK_BODY="{\"action\": \"ask\", \"interface\": \"audio-record\", \"pid\": $WAITER_SNAP_PID}"
RESULT="$(echo "$ASK_BODY" | snap debug api -X POST -H 'Content-Type: application/json' "/v2/interfaces/requests")"
echo "$RESULT" | grep '"status-code": 200'
echo "$RESULT" | grep '"outcome": "allow"'

# Trigger a fourth request
ASK_BODY="{\"action\": \"ask\", \"interface\": \"audio-record\", \"pid\": $WAITER_SNAP_PID}"
RESULT="$(echo "$ASK_BODY" | snap debug api -X POST -H 'Content-Type: application/json' "/v2/interfaces/requests")"
echo "$RESULT" | grep '"status-code": 200'
echo "$RESULT" | grep '"outcome": "allow"'

echo "Wait for the rule to expire"
sleep 10

# Trigger a fifth request
ASK_BODY="{\"action\": \"ask\", \"interface\": \"audio-record\", \"pid\": $WAITER_SNAP_PID}"
RESULT="$(echo "$ASK_BODY" | snap debug api -X POST -H 'Content-Type: application/json' "/v2/interfaces/requests")"
echo "$RESULT" | grep '"status-code": 200'
echo "$RESULT" | grep '"outcome": "deny"'

# Tell the waiter to stop waiting
touch "$TARGET_FILE"
wait "$WAITER_SHELL_PID"

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
