#!/bin/sh
# Check if the kernel is at least 4.4.0-67
if ! uname -r | perl -ne '/^(\d+)\.(\d+)\.(\d+)-(\d+)/ or exit 1; exit 1 if $1<4; exit 1 if $2<4; exit 1 if $3==0 && $4<67'; then
	exit 1
fi
