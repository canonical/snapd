#!/bin/sh

set -e

go get github.com/axw/gocov/gocov
go get gopkg.in/matm/v1/gocov-html

# pass alternative output dir in $1
OUTPUTDIR=${1:-$(pwd)}

(cd snappy &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-snappy.html)
(cd partition &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-partition.html)
(cd logger &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-logger.html)
(cd helpers &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-helpers.html)
(cd coreconfig &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-coreconfig.html)

(cd clickdeb &&
        $GOPATH/bin/gocov test | $GOPATH/bin/gocov-html > $OUTPUTDIR/cov-clickdeb.html)

echo "Coverage html reports are available in $OUTPUTDIR"
