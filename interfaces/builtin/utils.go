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

package builtin

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// AppLabelExpr returns the specification of the apparmor label describing
// all the apps bound to a given slot. The result has one of three forms,
// depending on how apps are bound to the slot:
//
// - "snap.$snap.$app" if there is exactly one app bound
// - "snap.$snap.{$app1,...$appN}" if there are some, but not all, apps bound
// - "snap.$snap.*" if all apps are bound to the slot
func appLabelExpr(apps map[string]*snap.AppInfo, snap *snap.Info) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `"snap.%s.`, snap.Name())
	if len(apps) == 1 {
		for appName := range apps {
			buf.WriteString(appName)
		}
	} else if len(apps) == len(snap.Apps) {
		buf.WriteByte('*')
	} else {
		appNames := make([]string, 0, len(apps))
		for appName := range apps {
			appNames = append(appNames, appName)
		}
		sort.Strings(appNames)
		buf.WriteByte('{')
		for _, appName := range appNames {
			buf.WriteString(appName)
			buf.WriteByte(',')
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	}
	buf.WriteByte('"')
	return buf.String()
}

func slotAppLabelExpr(slot *interfaces.Slot) string {
	return appLabelExpr(slot.Apps, slot.Snap)
}

func plugAppLabelExpr(plug *interfaces.Plug) string {
	return appLabelExpr(plug.Apps, plug.Snap)
}

// Function to support creation of udev snippet
func udevUsbDeviceSnippet(subsystem string, usbVendor int64, usbProduct int64, key string, data string) []byte {
	const udevHeader string = `IMPORT{builtin}="usb_id"`
	const udevDevicePrefix string = `SUBSYSTEM=="%s", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x"`
	const udevSuffix string = `, %s+="%s"`

	var udevSnippet bytes.Buffer
	udevSnippet.WriteString(udevHeader + "\n")
	udevSnippet.WriteString(fmt.Sprintf(udevDevicePrefix, subsystem, usbVendor, usbProduct))
	udevSnippet.WriteString(fmt.Sprintf(udevSuffix, key, data))
	udevSnippet.WriteString("\n")
	return udevSnippet.Bytes()
}

// Function to create an udev TAG, essentially the cgroup name for
// the snap application.
// @param snapName is the name of the snap
// @param appName is the name of the application
// @return string "snap_<snap name>_<app name>"
func udevSnapSecurityName(snapName string, appName string) string {
	return fmt.Sprintf(`snap_%s_%s`, snapName, appName)
}
