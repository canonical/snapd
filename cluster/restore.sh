#!/bin/bash
multipass list | grep 'host-[[:digit:]]' | awk '{ print $1 }' | xargs -I{} multipass restore {}.setup
multipass list | grep 'host-[[:digit:]]' | awk '{ print $1 }' | xargs multipass start
