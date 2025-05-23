#!/bin/sh -exu

echo 0 > /sys/kernel/config/gpio-sim/snaptest/live
rmdir /sys/kernel/config/gpio-sim/snaptest/gpio-bank1
rmdir /sys/kernel/config/gpio-sim/snaptest/gpio-bank0
rmdir /sys/kernel/config/gpio-sim/snaptest
rmmod gpio-sim
