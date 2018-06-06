#!/bin/bash
# shellcheck disable=SC2034

gadget_name=$(snap list | grep 'gadget$' | awk '{ print $1 }')
kernel_name=$(snap list | grep 'kernel$' | awk '{ print $1 }')

core_name="$(snap known model | grep base | cut -f2 -d: | tr -d '[:space:]')"
if [ -z "$core_name" ]; then
    core_name="core"
fi
