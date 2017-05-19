#!/bin/sh

set -eu

if [ -z "$(which govendor)" ];then
	echo Installing govendor
	go get -u github.com/kardianos/govendor
fi
export PATH=$PATH:$GOPATH/bin

echo Obtaining dependencies
govendor sync

unused="$(govendor list +unused)"
if [ "$unused" != "" ]; then
    echo "Found unused ./vendor packages:"
    echo "$unused"
    echo "Please fix via 'govendor remove +unused'"
    exit 1
fi
