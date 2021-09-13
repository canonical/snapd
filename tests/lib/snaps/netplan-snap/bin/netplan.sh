#!/bin/sh

set -e

# netplan should be on $PATH from the base snap at /usr/sbin/netplan
netplan "$@"
