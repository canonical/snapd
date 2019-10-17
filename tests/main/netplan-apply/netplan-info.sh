#!/bin/sh -e

exec busctl call --system io.netplan.Netplan /io/netplan/Netplan io.netplan.Netplan Info
