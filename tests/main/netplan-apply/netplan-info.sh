#!/bin/sh -e

busctl call --system io.netplan.Netplan /io/netplan/Netplan io.netplan.Netplan Info
