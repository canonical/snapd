#!/bin/bash

set -eu

gen() {
    suffix=""

    if [ "${2}" = 4096 ]; then
        suffix="${suffix}_4k"
    fi
    if [ "${1}" != normal ]; then
        suffix="${suffix}_${1}"
    fi

    case "${1}" in
        big)
            table_length=256
            ;;
        small)
            table_length=32
            ;;
        normal)
            table_length=128
            ;;
    esac

    truncate --size 128M image
    loop=$(losetup --sector-size "${2}" --show -f image)
    sfdisk "${loop}" <<EOF
label: gpt
table-length: ${table_length}
EOF
    dd if="${loop}" of="gpt_header${suffix}" bs="${2}" count=2
    # 128M - 1 block
    dd if="${loop}" skip=$((128*1024*1024/${2}-1)) of="gpt_footer${suffix}" bs="${2}" count=1
    losetup -d "${loop}"
    rm image
}

gen normal 512
gen big 512
gen small 512
gen normal 4096
gen big 4096
gen small 4096
