#!/bin/bash

check_connected() {
    snap="$1"
    interface="$2"
    snap interfaces | grep -q -E ":$interface +$snap"
}

check_disconnected() {
    snap="$1"
    interface="$2"
    snap interfaces | grep -q -E "\- +$snap:$interface"
}
