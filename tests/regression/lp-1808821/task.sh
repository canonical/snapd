#!/bin/sh -e
snap pack test-snapd-app
snap install --dangerous ./test-snapd-app_1_all.snap
snap run test-snapd-app.sh -c 'cat /usr/share/java/stub/stub.txt' | MATCH STUB
snap install --dangerous ./test-snapd-app_1_all.snap
snap run test-snapd-app.sh -c 'cat /usr/share/java/stub/stub.txt' | MATCH STUB
snap install --dangerous ./test-snapd-app_1_all.snap
snap run test-snapd-app.sh -c 'cat /usr/share/java/stub/stub.txt' | MATCH STUB
