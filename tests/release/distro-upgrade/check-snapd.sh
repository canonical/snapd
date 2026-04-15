#!/bin/bash

set -euxo pipefail

systemctl is-active snapd.service
systemctl is-active snapd.socket

tests.invariant check crashed-snap-confine
tests.invariant check broken-snaps

snap version | grep -q "$(cat snap-version.txt)"

snap debug confinement | MATCH "strict"

snap connections go-example-webserver > tmp-webserver-connectionts.txt
diff -u webserver-connections.txt tmp-webserver-connectionts.txt

tests.systemd wait-for-service -n 30 --state active snap.go-example-webserver.webserver.service
curl --fail --silent --show-error -o /dev/null localhost:8081

snap list > tmp-snap-list.txt
diff -u snap-list.txt tmp-snap-list.txt

test-snapd-sh.sh -c 'echo Hello' | MATCH "Hello"
test-snapd-sh.sh -c 'env' | MATCH "SNAP_NAME=test-snapd-sh"

echo Hello > /var/tmp/myevil.txt
if test-snapd-sh.sh -c 'cat /var/tmp/myevil.txt'; then
    exit 1
fi

test_snapd_wellknown1 | MATCH "ok wellknown 1"
test_snapd_wellknown2 | MATCH "ok wellknown 2"
snap aliases|MATCH "test-snapd-auto-aliases.wellknown1 +test_snapd_wellknown1 +-"
snap aliases|MATCH "test-snapd-auto-aliases.wellknown2 +test_snapd_wellknown2 +-"

test-snapd-classic-confinement.recurse 5
