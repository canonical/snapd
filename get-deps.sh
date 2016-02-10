#!/bin/sh

set -eu

echo Installing godeps
go get launchpad.net/godeps
export PATH=$PATH:$GOPATH/bin

echo Obtaining dependencies
godeps -u dependencies.tsv
