#!/bin/bash
gadget_name=$(snap list | sed -n 's/^\(pc\|pi[23]\|dragonboard\) .*/\1/p')
kernel_name=$gadget_name-kernel
core_name=$(snap list | awk '/^(ubuntu-)?core / {print $1; exit}')

if [ "$kernel_name" = "pi3-kernel" ]; then
    kernel_name=pi2-kernel
fi
