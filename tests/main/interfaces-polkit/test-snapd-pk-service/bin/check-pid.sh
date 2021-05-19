#!/bin/sh

pid="$1"
action_id="$2"

uid="$(stat --format %u "/proc/$pid")"
pid_start="$(sed 's/([^)]*)/x/' < "/proc/$pid/stat" | cut -d ' ' -f 22)"

subject="('unix-process', {'pid': <uint32 ${pid}>, 'start-time': <uint64 ${pid_start}>, 'uid': <uint32 ${uid}>})"

exec gdbus call --system \
     --dest org.freedesktop.PolicyKit1 \
     --object-path /org/freedesktop/PolicyKit1/Authority \
     --method org.freedesktop.PolicyKit1.Authority.CheckAuthorization \
     "$subject" "'$action_id'" "@a{ss} {}" 0 "''"
