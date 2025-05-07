#!/bin/sh -exu

CONFIG_BASE=/sys/kernel/config/gpio-sim
CHIP_NAME=snaptest

/usr/sbin/modprobe gpio-sim

mkdir -p "${CONFIG_BASE}/${CHIP_NAME}"
mkdir -p "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0"
echo "gpio-bank0" > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0/label"
echo 8 > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0/num_lines"

mkdir -p "${CONFIG_BASE}/${CHIP_NAME}"
mkdir -p "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1"
echo "gpio-bank1" > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1/label"
echo 8 > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1/num_lines"

echo 1 > /sys/kernel/config/gpio-sim/snaptest/live
