#!/bin/sh
# Check if the kernel is at least 4.4.0-67
if [ "$(uname -r | perl -ne 'print $1 if /^([0-9]+)\./')" -lt 4 ]; then
	exit 1
fi
if [ "$(uname -r | perl -ne 'print $1 if /^[0-9]+\.([0-9]+)\./')" -lt 4 ]; then
	exit 1
fi
if [ "$(uname -r | perl -ne 'print $1 if /^[0-9]+\.[0-9]+\.[0-9]+-([0-9]+)/')" -lt 67 ]; then
	exit 1
fi
