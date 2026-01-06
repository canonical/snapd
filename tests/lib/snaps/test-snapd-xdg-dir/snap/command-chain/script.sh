#!/bin/bash
if [ -d "$XDG_RUNTIME_DIR" ];
then
    exit 0
else
    exit 255
fi
