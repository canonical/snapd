#!/bin/sh

set -eu

if ! which govendor >/dev/null;then
    export PATH="$PATH:${GOPATH%%:*}/bin"

    if ! which govendor >/dev/null;then
	    echo Installing govendor
	    go get -u github.com/kardianos/govendor
    fi
fi

echo Obtaining dependencies
govendor sync

unused="$(govendor list +unused)"
if [ "$unused" != "" ]; then
    echo "Found unused ./vendor packages:"
    echo "$unused"
    echo "Please fix via 'govendor remove +unused'"
    exit 1
fi
