#!/bin/bash

set -eu

[ -n "${1}" ]

[ -d "${1}" ] || mkdir -p "${1}"
for source in /sys/firmware/efi/efivars/Boot{Order,Current,'[0-9][0-9][0-9][0-9]'}-8be4df61-93ca-11d2-aa0d-00e098032b8c; do
    remote.pull "${source}" "${1}"
done
