#!/bin/sh -e

pid="$1"
action_id="$2"

uid="$(stat --format %u "/proc/$pid")"
pid_start="$(sed 's/([^)]*)/program/' < "/proc/$pid/stat" | cut -d ' ' -f 22)"

exec busctl --system call \
     org.freedesktop.PolicyKit1 /org/freedesktop/PolicyKit1/Authority \
     org.freedesktop.PolicyKit1.Authority CheckAuthorization \
     '(sa{sv})sa{ss}us' \
     unix-process 3 pid u "$pid" start-time t "$pid_start" uid u "$uid" \
     "$action_id" \
     0 \
     0 \
     ""
