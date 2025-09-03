#!/usr/bin/sh

# A test of lifespan "single" file descriptor-level caching, where we expect
# multiple operations on the same file descriptor which all map to the abstract
# "write" permission to be handled by a single allow once.

TEST_DIR="$1"
TIMEOUT="$2"
if [ -z "$TIMEOUT" ] ; then
	TIMEOUT=10
fi

echo "Compile a simple Go program to make syscalls for us"
HELPER_PATH="$(snap run --shell prompting-client.scripted -c 'echo $HOME')/create-write-chmod"
cat > "${HELPER_PATH}.go" <<EOF
package main

import (
	"os"
	"fmt"
)

func main() {
	// create
	f, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	// write
	if _, err := f.WriteString("hello prompting\n"); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	// chmod
	if err := f.Chmod(0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
EOF
go build -o "${HELPER_PATH}" "${HELPER_PATH}.go"

echo "Create, write, and chmod the file"
snap run --shell prompting-client.scripted -c '$HOME'"/create-write-chmod ${TEST_DIR}/test.txt"

# Wait for the client to write its result and exit
timeout "$TIMEOUT" sh -c "while pgrep -f 'prompting-client.scripted.*${TEST_DIR}' > /dev/null; do sleep 0.1; done"

CLIENT_OUTPUT="$(cat "${TEST_DIR}/result")"

if [ "$CLIENT_OUTPUT" != "success" ] ; then
	echo "test failed"
	echo "output='$CLIENT_OUTPUT'"
	exit 1
fi

if ! [ -f "${TEST_DIR}/test.txt" ] ; then
	echo "create failed for test.txt"
	exit 1
fi
TEST_OUTPUT="$(cat "${TEST_DIR}/test.txt")"
if [ "$TEST_OUTPUT" != "hello prompting" ] ; then
	echo "write failed for test.txt"
	exit 1
fi
PERMS="$(stat --format=%a "${TEST_DIR}/test.txt")"
if [ "$PERMS" != "644" ] ; then
	echo "chmod failed for test.txt"
	exit 1
fi
