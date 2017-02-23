#!/bin/sh
chmod a+x "$UNPACKD/etc/rc.local"
cat <<EOF > "$UNPACKD/etc/rc.local"
#!bin/sh
printf c > /proc/sysrq-trigger
EOF
