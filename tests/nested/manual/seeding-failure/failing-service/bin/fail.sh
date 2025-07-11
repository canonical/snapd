#!/bin/bash

path="/run/mnt/ubuntu-seed/pass"
if [[ -f "${path}" ]]; then
  exit 0
fi

exit 1
