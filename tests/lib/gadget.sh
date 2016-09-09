#!/bin/sh
get_gadget_name(){
    snap list | grep '^pc \|^pi2 ' | head -n 1 | cut -d ' ' -f 1
}
