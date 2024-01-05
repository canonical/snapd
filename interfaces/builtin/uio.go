// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

// https://www.kernel.org/doc/html/latest/driver-api/uio-howto.html
const uioSummary = `allows access to specific uio device`

const uioBaseDeclarationSlots = `
  uio:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

type uioInterface struct{}

func (iface *uioInterface) Name() string {
	return "uio"
}

func (iface *uioInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              uioSummary,
		BaseDeclarationSlots: uioBaseDeclarationSlots,
	}
}

var uioPattern = regexp.MustCompile(`^/dev/uio[0-9]+$`)

const invalidUioDeviceNodeSlotPathErrFmt = "slot %q path attribute must be a valid UIO device node"

func (iface *uioInterface) path(slotRef *interfaces.SlotRef, attrs interfaces.Attrer) (string, error) {
	return verifySlotPathAttribute(slotRef, attrs, uioPattern, invalidUioDeviceNodeSlotPathErrFmt)
}

func (iface *uioInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_, err := verifySlotPathAttribute(&interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}, slot, uioPattern, invalidUioDeviceNodeSlotPathErrFmt)
	return err
}

func (iface *uioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.AddSnippet(fmt.Sprintf("%s rw,", path))
	// Assuming sysfs_base is /sys/class/uio/uio[0-9]+ where the leaf directory
	// name matches /dev/uio[0-9]+ device name, the following files exists or
	// may exist:
	//  - $sysfs_base/{name,version,event}
	//  - $sysfs_base/maps/map[0-9]+/{addr,name,offset,size}
	//  - $sysfs_base/portio/port[0-9]+/{name,start,size,porttype}
	// The expression below matches them all as they all may be required for
	// userspace drivers to operate.
	//
	// While it is more accurate to use:
	//
	//   "/sys/devices/platform/**/uio/%s/** r,", strings.TrimPrefix(path, "/dev/")
	//
	// multiple interface connections will result in overlapping deep
	// globs of the form:
	//
	//   /sys/devices/platform/**/uio/uio1/** r,
	//   /sys/devices/platform/**/uio/uio2/** r,
	//   /sys/devices/platform/**/uio/uioN/** r,
	//
	// which are computationally difficult to de-duplicate provided
	// large enough N. Instead, grant read only access to all uio
	// sysfs files and control writable access to the specific
	// device node in /dev. Use AddDeduplicatedSnippet() for clarity
	// in the resulting rules.
	spec.AddDeduplicatedSnippet("/sys/devices/platform/**/uio/uio[0-9]** r,  # common rule for all uio connections")

	// Allow uio configuration
	uioConfigPath := filepath.Join("/sys/class/uio/", strings.TrimPrefix(path, "/dev/"), "/device/config")
	dereferencedPath, err := evalSymlinks(uioConfigPath)
	if err != nil && os.IsNotExist(err) {
		// This should not block the interface connection operation
		logger.Noticef("cannot configure not existing uio device config file %s", uioConfigPath)
		return nil
	}
	if err != nil {
		return err
	}
	spec.AddSnippet(fmt.Sprintf("%s rwk,", dereferencedPath))
	return nil
}

func (iface *uioInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.TagDevice(fmt.Sprintf(`SUBSYSTEM=="uio", KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *uioInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&uioInterface{})
}
