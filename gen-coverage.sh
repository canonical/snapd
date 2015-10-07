#!/bin/sh

set -e

go get github.com/axw/gocov/gocov
go get gopkg.in/matm/v1/gocov-html

# pass alternative output dir in $1
OUTPUTDIR=${1:-$(pwd)}

for d in pkg/snapfs snappy partition logger helpers coreconfig clickdeb priv release oauth; do
    (cd $d &&
      $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-$(echo $d|sed -r 's#/#_#').html)
done

echo "Coverage html reports are available in $OUTPUTDIR"
