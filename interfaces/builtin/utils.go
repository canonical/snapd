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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// The maximum number of Usb bInterfaceNumber.
const UsbMaxInterfaces = 32

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
func udevUsbDeviceSnippet(subsystem string, usbVendor int64, usbProduct int64, usbIterfaceNumber int64, key string, data string) string {
	const udevHeader string = `IMPORT{builtin}="usb_id"`
	const udevDevicePrefix string = `SUBSYSTEM=="%s", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x", ENV{ID_USB_INTERFACE_NUM}=="%02d"`
	const udevSuffix string = `, %s+="%s"`

	var udevSnippet bytes.Buffer
	udevSnippet.WriteString(udevHeader + "\n")
	if usbIterfaceNumber < 0 || usbIterfaceNumber >= UsbMaxInterfaces {
		udevDevicePrefixNoInterface := strings.Replace(udevDevicePrefix, `, ENV{ID_USB_INTERFACE_NUM}=="%02d"`, "", -1)
		udevSnippet.WriteString(fmt.Sprintf(udevDevicePrefixNoInterface, subsystem, usbVendor, usbProduct))
	} else {
		udevSnippet.WriteString(fmt.Sprintf(udevDevicePrefix, subsystem, usbVendor, usbProduct, usbIterfaceNumber))
	}
	udevSnippet.WriteString(fmt.Sprintf(udevSuffix, key, data))
	return udevSnippet.String()
}

// Function to create an udev TAG, essentially the cgroup name for
// the snap application.
// @param snapName is the name of the snap
// @param appName is the name of the application
// @return string "snap_<snap name>_<app name>"
func udevSnapSecurityName(snapName string, appName string) string {
	return fmt.Sprintf(`snap_%s_%s`, snapName, appName)
}

// sanitizeSlotReservedForOS checks if slot is of type os.
func sanitizeSlotReservedForOS(iface interfaces.Interface, slot *interfaces.Slot) error {
	if slot.Snap.Type != snap.TypeOS {
		return fmt.Errorf("%s slots are reserved for the core snap", iface.Name())
	}
	return nil
}

// sanitizeSlotReservedForOSOrGadget checks if the slot is of type os or gadget.
func sanitizeSlotReservedForOSOrGadget(iface interfaces.Interface, slot *interfaces.Slot) error {
	if slot.Snap.Type != snap.TypeOS && slot.Snap.Type != snap.TypeGadget {
		return fmt.Errorf("%s slots are reserved for the core and gadget snaps", iface.Name())
	}
	return nil
}

// sanitizeSlotReservedForOSOrApp checks if the slot is of type os or app.
func sanitizeSlotReservedForOSOrApp(iface interfaces.Interface, slot *interfaces.Slot) error {
	if slot.Snap.Type != snap.TypeOS && slot.Snap.Type != snap.TypeApp {
		return fmt.Errorf("%s slots are reserved for the core and app snaps", iface.Name())
	}
	return nil
}
