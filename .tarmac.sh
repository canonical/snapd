#!/bin/sh

set -ev

# we always run in a fresh dir in tarmac
export GOPATH=$(mktemp -d)
trap 'rm -rf "$GOPATH"' EXIT

# this is a hack, but not sure tarmac is golang friendly
mkdir -p $GOPATH/src/launchpad.net/snappy
cp -a . $GOPATH/src/launchpad.net/snappy/
cd $GOPATH/src/launchpad.net/snappy

./run-checks
