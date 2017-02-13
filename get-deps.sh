#!/bin/sh

set -eu

if [ -z "$(which govendor)" ];then
	echo Installing govendor
	go get -u github.com/kardianos/govendor
fi
export PATH=$PATH:$GOPATH/bin

echo Obtaining dependencies
govendor sync
