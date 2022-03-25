#!/bin/bash -e

INSTANCE="$1"

if [ -z "$INSTANCE" ]; then
    echo "first argument must be the GCE instance"
    exit 1
fi

# wait for the instance to exist
until gcloud compute --project=snapd-spread instances describe "$INSTANCE" --zone=us-east1-b >/dev/null 2>&1; do 
    echo "waiting for instance to exist"
    sleep 1
done

backgroundscript=/tmp/background-$RANDOM.sh
cat >> "$backgroundscript" << 'EOF'
INSTANCE="$1"
next=0
truncate -s0 console-output.txt
while true; do
    next=$(
        gcloud compute \
            --project=snapd-spread \
            instances get-serial-port-output "$INSTANCE" \
            --start="$next" \
            --zone=us-east1-b 3>&1 1>"console-output-bits.txt" 2>&3- | grep -Po -- '--start=\K[0-9]+')
    trimmedConsoleSnippet="$(sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' < console-output-bits.txt)"
    if [ -n "$trimmedConsoleSnippet" ]; then
        echo "$trimmedConsoleSnippet" >> console-output.txt
    fi
    sleep 1
done
EOF

# start collecting the console output
bash "$backgroundscript" "$INSTANCE" &


trap "exit" INT TERM
trap "kill 0" EXIT

# wait for it to appear
until [ -f console-output.txt ]; do
    sleep 1
done

# watch it
tail -f console-output.txt