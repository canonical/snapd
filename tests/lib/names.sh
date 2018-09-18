#!/bin/bash
# shellcheck disable=SC2034

gadget_name=$(snap list | grep 'gadget$' | awk '{ print $1 }')
kernel_name=$(snap list | grep 'kernel$' | awk '{ print $1 }')

core_name="$(snap known model | awk '/^base: / { print $2 }' || true)"
if [ -z "$core_name" ]; then
    core_name="core"
fi
