// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package interfaces

import (
	"fmt"
)

// securityHelper is an interface for common aspects of generating security files.
type securityHelper interface {
	securitySystem() SecuritySystem
	pathForApp(snapName, snapVersion, snapOrigin, appName string) string
	headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte
	footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte
}

// uDev is a security subsystem that writes additional udev rules (one per snap).
//
// Each rule looks like this:
//
// KERNEL=="hiddev0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="http.GET.snap"
//
// NOTE: This interacts with ubuntu-core-launcher.
//
// This tag is picked up by /lib/udev/rules.d/80-snappy-assign.rules which in
// turn runs /lib/udev/snappy-app-dev script, which re-configures the device
// cgroup at /sys/fs/cgroup/devices/snappy.$SNAPPY_APP for the acl
// "c $major:$minor rwm" for character devices and "b $major:$minor rwm" for
// block devices.
//
// $SNAPPY_APP is always computed with SecurityTagForApp()
//
// The control group is created by ubuntu-app-launcher.
type uDev struct{}

func (udev *uDev) securitySystem() SecuritySystem {
	return SecurityUDev
}

func (udev *uDev) pathForApp(snapName, snapVersion, snapOrigin, appName string) string {
	// NOTE: we ignore appName so effectively udev rules apply to entire snap.
	return fmt.Sprintf("/etc/udev/rules.d/70-%s.rules", SecurityTagForApp(snapName, appName))
}

func (udev *uDev) headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return nil // udev doesn't require a header
}

func (udev *uDev) footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return nil // udev doesn't require a footer
}

// dBus is a security subsystem that writes DBus "firewall" configuration files.
//
// Each configuration is an XML file with <policy>...</policy>. Particular
// security snippets must be complete policy declarations.
//
// NOTE: This interacts with systemd.
// TODO: Explain how this works (security).
type dBus struct{}

func (dbus *dBus) securitySystem() SecuritySystem {
	return SecurityDBus
}

func (dbus *dBus) pathForApp(snapName, snapVersion, snapOrigin, appName string) string {
	// XXX: Is the name of this file relevant or can everything be contained
	// in particular snippets?
	// XXX: At this level we don't know the bus name.
	return fmt.Sprintf("/etc/dbus-1/system.d/%s.conf", SecurityTagForApp(snapName, appName))
}

func (dbus *dBus) headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return []byte("" +
		"<!DOCTYPE busconfig PUBLIC\n" +
		" \"-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN\"\n" +
		" \"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd\">\n" +
		"<busconfig>\n")
}

func (dbus *dBus) footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return []byte("" +
		"</busconfig>\n")
}
