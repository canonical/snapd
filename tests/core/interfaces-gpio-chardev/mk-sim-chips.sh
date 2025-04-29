#!/bin/bash

CONFIG_BASE=/sys/kernel/config/gpio-sim
CHIP_NAME=snaptest

/usr/sbin/modprobe gpio-sim || true

mkdir -p "${CONFIG_BASE}/${CHIP_NAME}" || true
mkdir -p "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0" || true
echo "gpio-bank0" > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0/label" || true
echo 8 > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank0/num_lines" || true

mkdir -p "${CONFIG_BASE}/${CHIP_NAME}" || true
mkdir -p "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1" || true
echo "gpio-bank1" > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1/label" || true
echo 8 > "${CONFIG_BASE}/${CHIP_NAME}/gpio-bank1/num_lines" || true

echo 1 > /sys/kernel/config/gpio-sim/snaptest/live || true
