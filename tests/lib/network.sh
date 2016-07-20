#!/bin/sh
get_default_iface(){
    echo "$(ip route get 8.8.8.8 | awk '{ print $5; exit }')"
}
