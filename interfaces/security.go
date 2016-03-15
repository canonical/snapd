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
	"bytes"
	"fmt"
	"strings"
)

// WrapperNameForApp returns the name of the wrapper for a given application.
//
// A wrapper is a generated helper executable that assists in setting up
// environment for running a particular application.
//
// In general, the wrapper has the form: "$snap.$app". When both snap name and
// app name are the same then the tag is simplified to just "$snap".
func WrapperNameForApp(snapName, appName string) string {
	if appName == snapName {
		return snapName
	}
	return fmt.Sprintf("%s.%s", snapName, appName)
}

// SecurityTagForApp returns the unified tag used for all security systems.
//
// In general, the tag has the form: "$snap.$app.snap". When both snap name and
// app name are the same then the tag is simplified to just "$snap.snap".
func SecurityTagForApp(snapName, appName string) string {
	return fmt.Sprintf("%s.snap", WrapperNameForApp(snapName, appName))
}

// securityHelper is an interface for common aspects of generating security files.
type securityHelper interface {
	securitySystem() SecuritySystem
	pathForApp(snapName, snapVersion, snapOrigin, appName string) string
	headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte
	footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte
}

// appArmor is a security subsystem that writes apparmor profiles.
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specified an identifier that is relevant to the kernel. The
// identifier can be either the full path of the executable or an abstract
// identifier not related to the executable name.
//
// A file containing an apparmor profile has to be parsed, compiled and loaded
// into the running kernel using apparmor_parser. After this is done the actual
// file is irrelevant and can be removed. To improve performance certain
// command line options to apparmor_parser can be used to cache compiled
// profiles across reboots.
//
// NOTE: ubuntu-core-launcher only uses the profile identifier. It doesn't handle
// loading the profile into the kernel or compiling it from source.
type appArmor struct{}

func (aa *appArmor) securitySystem() SecuritySystem {
	return SecurityAppArmor
}

func (aa *appArmor) pathForApp(snapName, snapVersion, snapOrigin, appName string) string {
	return fmt.Sprintf("/var/lib/snappy/apparmor/profiles/%s",
		SecurityTagForApp(snapName, appName))
}

func (aa *appArmor) headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	header := string(appArmorHeader)
	vars := aa.varsForApp(snapName, snapVersion, snapOrigin, appName)
	profileAttach := aa.profileAttachForApp(snapName, snapVersion, snapOrigin, appName)
	header = strings.Replace(header, "###VAR###\n", vars, 1)
	header = strings.Replace(header, "###PROFILEATTACH###", profileAttach, 1)
	return []byte(header)
}

func (aa *appArmor) varsForApp(snapName, snapVersion, snapOrigin, appName string) string {
	return "\n" +
		"# Specified profile variables\n" +
		fmt.Sprintf("@{APP_APPNAME}=\"%s\"\n", appName) +
		fmt.Sprintf("@{APP_ID_DBUS}=\"%s\"\n", dbusPath(
			fmt.Sprintf("%s.%s_%s_%s", snapName, snapOrigin, appName, snapVersion))) +
		fmt.Sprintf("@{APP_PKGNAME_DBUS}=\"%s\"\n", dbusPath(fmt.Sprintf("%s.%s", snapName, snapOrigin))) +
		fmt.Sprintf("@{APP_PKGNAME}=\"%s\"\n", fmt.Sprintf("%s.%s", snapName, snapOrigin)) +
		fmt.Sprintf("@{APP_VERSION}=\"%s\"\n", snapVersion) +
		"@{INSTALL_DIR}=\"{/snaps,/gadget}\"\n"
}

func (aa *appArmor) profileAttachForApp(snapName, snapVersion, snapOrigin, appName string) string {
	return fmt.Sprintf("profile \"%s\"", SecurityTagForApp(snapName, appName))
}

// Generate a string suitable for use in a DBus object
func dbusPath(s string) string {
	const allowed = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`
	buf := bytes.NewBuffer(make([]byte, 0, len(s)))

	for _, c := range []byte(s) {
		if strings.IndexByte(allowed, c) >= 0 {
			fmt.Fprintf(buf, "%c", c)
		} else {
			fmt.Fprintf(buf, "_%02x", c)
		}
	}

	return buf.String()
}

func (aa *appArmor) footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return []byte("}\n")
}

// secComp is a security subsystem that writes additional seccomp rules.
//
// Rules use a simple line-oriented record structure.  Each line specifies a
// system call that is allowed. Lines starting with "deny" specify system
// calls that are explicitly not allowed. Lines starting with '#' are treated
// as comments and are ignored.
//
// NOTE: This subsystem interacts with ubuntu-core-launcher. The launcher reads
// a single profile from a specific path, parses it and loads a seccomp profile
// (using Berkley packet filter as a low level mechanism).
type secComp struct{}

func (sc *secComp) securitySystem() SecuritySystem {
	return SecuritySecComp
}

func (sc *secComp) pathForApp(snapName, snapVersion, snapOrigin, appName string) string {
	// NOTE: This path has to be synchronized with ubuntu-core-launcher.
	return fmt.Sprintf("/var/lib/snappy/seccomp/profiles/%s",
		SecurityTagForApp(snapName, appName))
}

var secCompHeader = []byte(defaultSecCompTemplate)
var appArmorHeader = []byte(strings.TrimRight(defaultAppArmorTemplate, "\n}"))

func (sc *secComp) headerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return secCompHeader
}

func (sc *secComp) footerForApp(snapName, snapVersion, snapOrigin, appName string) []byte {
	return nil // seccomp doesn't require a footer
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
