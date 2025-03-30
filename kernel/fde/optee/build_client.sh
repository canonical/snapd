#!/bin/bash

rm -rf ./optee_client
git clone https://github.com/OP-TEE/optee_client.git --branch 4.5.0 --depth 1
make -C ./optee_client WITH_TEEACL=0 CROSS_COMPILE=aarch64-linux-gnu- O=./install install
