#!/bin/sh

set -ev

# we always run in a fresh dir in tarmac
export GOPATH=$(mktemp -d)
trap 'rm -rf "$GOPATH"' EXIT

# this is a hack, but not sure tarmac is golang friendly
mkdir -p $GOPATH/src/github.com/snapcore/snapd
cp -a . $GOPATH/src/github.com/snapcore/snapd/
cd $GOPATH/src/github.com/snapcore/snapd

sh -v ./run-checks
