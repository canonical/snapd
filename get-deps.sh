#!/bin/sh

set -e

echo Obtaining dependencies
go mod vendor

echo Obtaining c-dependencies
(cd c-vendor && ./vendor.sh)

# TODO: import script that ensures that the "go.mod" vendor dir is tidy
# https://github.com/edgexfoundry/edgex-go/commit/2c7e513168ecd884ba7252d8253b100953d1695c
