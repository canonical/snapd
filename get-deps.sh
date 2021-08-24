#!/bin/sh

set -e


echo Obtaining dependencies
go mod vendor

echo Obtaining c-dependencies
(cd c-vendor && ./vendor.sh)

# TODO: port to go mod
# if [ "$1" != "--skip-unused-check" ]; then
#     unused="$(govendor list +unused)"
#     if [ "$unused" != "" ]; then
#         echo "Found unused ./vendor packages:"
#         echo "$unused"
#         echo "Please fix via 'govendor remove +unused'"
#         exit 1
#     fi
# fi
