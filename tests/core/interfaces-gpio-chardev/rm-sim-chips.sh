#!/bin/sh

echo 0 > /sys/kernel/config/gpio-sim/snaptest/live || true
rmdir /sys/kernel/config/gpio-sim/snaptest/gpio-bank1 || true
rmdir /sys/kernel/config/gpio-sim/snaptest/gpio-bank0 || true
rmdir /sys/kernel/config/gpio-sim/snaptest || true
rmmod gpio-sim || true
