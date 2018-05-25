#!/bin/sh

set -e

HERE=$(pwd)

if [ "$GOPATH" = "" ]; then
    export GOPATH=$(mktemp -d)
    trap "rm -rf $GOPATH" EXIT

    mkdir -p $GOPATH/src/github.com/snapcore/
    ln -s $(pwd) $GOPATH/src/github.com/snapcore/snapd
    cd $GOPATH/src/github.com/snapcore/snapd
fi

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
