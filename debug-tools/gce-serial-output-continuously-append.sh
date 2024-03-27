#!/bin/bash -e

INSTANCE="$1"

if [ -z "$INSTANCE" ]; then
    echo "first argument must be the GCE instance (for example, mar280632-365378)"
    exit 1
fi

# wait for the instance to exist
until gcloud compute --project=snapd-spread instances describe "$INSTANCE" --zone=us-east1-b >/dev/null 2>&1; do
    echo "waiting for instance to exist"
    sleep 1
done

OUTPUT_FILE="console-output-$INSTANCE.txt"

backgroundscript=$(mktemp --suffix=.gce-watcher)
cat >> "$backgroundscript" << 'EOF'
INSTANCE="$1"
OUTPUT_FILE="$2"
next=0
truncate -s0 "$OUTPUT_FILE"
while true; do
    # The get-serial-port-output command will print on the stdout the new lines
    # that the machine emitted on the serial console since the last time it was
    # queried. The bookmark is the number ("$next") that we pass with the
    # --start parameter; this number is printed by gcloud to the stderr in this
    # form:
    #
    #   Specify --start=130061 in the next get-serial-port-output invocation to get only the new output starting from here.
    #
    # In the subshell below we compute the value of the "$next" variable: we
    # store the original stdout into the console-output-bits.txt file, then
    # (via a third file descriptor, not to mess with the original stdout) we
    # redirect the stderr into the stdout and use "grep" to extract the
    # suggested value for the --start parameter.
    next=$(
        gcloud compute \
            --project=snapd-spread \
            instances get-serial-port-output "$INSTANCE" \
            --start="$next" \
            --zone=us-east1-b 3>&1 1>"${OUTPUT_FILE}-bits.txt" 2>&3- | grep -Po -- '--start=\K[0-9]+')
    trimmedConsoleSnippet="$(sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' < ${OUTPUT_FILE}-bits.txt)"
    if [ -n "$trimmedConsoleSnippet" ]; then
        echo "$trimmedConsoleSnippet" >> "$OUTPUT_FILE"
    fi
    sleep 1
done
EOF

# start collecting the console output
bash "$backgroundscript" "$INSTANCE" "$OUTPUT_FILE" &

on_exit() {
    # Restore the signal handlers to avoid recursion
    trap - INT TERM QUIT EXIT
    rm -f "$OUTPUT_FILE" "$OUTPUT_FILE-bits.txt"
    # kill all processes in this group
    kill 0
}
trap "on_exit" INT TERM QUIT EXIT

# wait for it to appear
until [ -f "$OUTPUT_FILE" ]; do
    sleep 1
done

# watch it
tail -f "$OUTPUT_FILE"
