#!/bin/bash

gadget_name=$(snap list | sed -n 's/^\(pc\|pi[23]\|dragonboard\|cm3\) .*/\1/p')
kernel_name=$gadget_name-kernel

if [ "$kernel_name" = "pi3-kernel" ] || [ "$kernel_name" = "cm3-kernel" ]; then
    kernel_name=pi2-kernel
fi
