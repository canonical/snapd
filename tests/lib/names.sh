#!/bin/bash

gadget_name=$(snap list | grep 'gadget$' | awk '{ print $1 }')
kernel_name=$(snap list | grep 'kernel$' | awk '{ print $1 }')
