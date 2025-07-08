#!/bin/bash

path="/tmp/service-ran"
if [[ -f "${path}" ]]; then
  exit 0
fi

touch "${path}"
exit 1
