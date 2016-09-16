#!/bin/sh
get_gadget_name(){
    snap list | grep '^pc \|^pi2 \|^pi3 \|^dragonboard ' | head -n 1 | cut -d ' ' -f 1
}

get_kernel_name(){
    gadget=$(get_gadget_name)

    if [ "$gadget" = "pi3" ];then
        gadget="pi2"
    fi

    echo "${gadget}-kernel"
}
