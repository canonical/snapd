#!/bin/bash

set -eu

truncate --size 128M image
echo "label: gpt" | sfdisk image
dd if=image of=gpt_header bs=512 count=2
# 128M - 1 block
dd if=image skip=$((128*1024*2-1)) of=gpt_footer bs=512 count=1
rm image
