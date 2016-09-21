#!/bin/sh

set -eu

echo Installing govendor
go get -u github.com/kardianos/govendor
export PATH=$PATH:$GOPATH/bin

echo Obtaining dependencies
govendor sync
