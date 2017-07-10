#!/bin/bash

get_default_iface(){
    ip route get 8.8.8.8 | awk '{ print $5; exit }'
}
