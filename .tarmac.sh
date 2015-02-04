#!/bin/sh

set -ev

export GOPATH=$(mktemp -d)
trap 'rm -r "$GOPATH"' EXIT

echo Checking formatting
fmt=$(gofmt -l .)

if [ -n "$fmt" ]; then
    echo "Formatting wrong in following files"
    echo $fmt
    exit 1
fi

echo Obtaining dependencies
go get -d -v launchpad.net/snappy/...

# this is a hack, but not sure tarmac is golang friendly
rm -r $GOPATH/src/launchpad.net/snappy
mkdir $GOPATH/src/launchpad.net/snappy

cp -r . $GOPATH/src/launchpad.net/snappy/
cd $GOPATH/src/launchpad.net/snappy

echo Building
go build -v launchpad.net/snappy/...

echo Obtaining test dependencies
go get gopkg.in/check.v1

echo Running tests from $(pwd)
go test ./...
