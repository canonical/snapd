#!/bin/bash -e

# type: bare structure just is read directly from the disk device node
# start from 1M offset which is ubuntu core standard non-MBR structure start
sudo cat /dev/vdb | tail -c +1M | head -c 1024 | grep -a -q "type bare foo 2"

# no filesystem partition structure is read directly from the partition device 
# node
sudo cat /dev/vdb1 | grep -q -a "no fs foo 2"

# for the things with filesystems we have to mount them to check

# make partitions for the structures with filesystems
mkdir /tmp/foo2
mkdir /tmp/foo3

# mount them
sudo mount /dev/vdb2 /tmp/foo2
sudo mount /dev/vdb3 /tmp/foo3

# check the files - this has a declared filesystem in the gadget.yaml
grep -q "file foo DECLARED FS 2" /tmp/foo2/foo.txt

# this one doesn't have a declared filesystem in the gadget.yaml
grep -q "file foo 2" /tmp/foo3/foo.txt
