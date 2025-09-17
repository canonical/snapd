#!/bin/bash

# TODO: add set -u once xenial is not supported anymore
set -e
shopt -s nullglob

CREDS="${1}"
shift

SMBIOS_TYPE_11=()

for cred in "${CREDS}"/*; do
    name=$(basename "${cred}")
    value=$(base64 -w0 <"${cred}")
    SMBIOS_TYPE_11+=("value=io.systemd.credential.binary:${name}=${value}")
done

PARAM_SMBIOS=()
if [ ${#SMBIOS_TYPE_11[*]} -gt 0 ]; then
    PARAM_SMBIOS+=("-smbios" "$(
        IFS=","
        echo "type=11,${SMBIOS_TYPE_11[*]}"
    )")
fi

exec "$@" "${PARAM_SMBIOS[@]}"
