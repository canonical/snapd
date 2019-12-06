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
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

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

func (iface *uioInterface) path(slotRef *interfaces.SlotRef, attrs interfaces.Attrer) (string, error) {
	var path string
	if err := attrs.Attr("path", &path); err != nil || path == "" {
		return "", fmt.Errorf("slot %q must have a path attribute", slotRef)
	}
	path = filepath.Clean(path)
	if !uioPattern.MatchString(path) {
		return "", fmt.Errorf("%q is not a valid UIO device", path)
	}
	return path, nil
}

func (iface *uioInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_, err := iface.path(&interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}, slot)
	return err
}

func (iface *uioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.AddSnippet(fmt.Sprintf("%s rwm,", path))
	// Assuming sysfs_base is /sys/class/uio/uio[0-9]+ where the leaf directory
	// name matches /dev/uio[0-9]+ device name, the following files exists or
	// may exist:
	//  - $sysfs_base/{name,version,event}
	//  - $sysfs_base/maps/map[0-9]+/{addr,name,offset,size}
	//  - $sysfs_base/portio/port[0-9]+/{name,start,size,porttype}
	// The expression below matches them all as they all may be required for
	// userspace drivers to operate.
	spec.AddSnippet(fmt.Sprintf("/sys/devices/platform/**/uio/%s/** rw,", strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *uioInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *uioInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func (iface *uioInterface) HotplugDeviceDetected(di *hotplug.HotplugDeviceInfo) (*hotplug.ProposedSlot, error) {
	if di.Subsystem() != "uio" {
		return nil, nil
	}
	slot := hotplug.ProposedSlot{
		Name: strings.TrimPrefix(di.DeviceName(), "/dev/"),
		Attrs: map[string]interface{}{
			"path": di.DeviceName(),
		},
	}
	return &slot, nil
}

func (iface *uioInterface) HotplugKey(di *hotplug.HotplugDeviceInfo) (snap.HotplugKey, error) {
	// We are interested in the part after "/sys/devices/platform/" but before
	// the following "/uio/uioN/". Parts will be as follows:
	// ["", "sys", "devices", "platform", "the-thing-we-want", "uio", "more-things", ...]
	parts := strings.Split(di.DevicePath(), "/")
	if len(parts) < 5 {
		return "", fmt.Errorf("unexpected device path for UIO device: %q", di.DevicePath())
	}
	key := sha256.New()
	key.Write([]byte("version"))
	key.Write([]byte{0})
	key.Write([]byte("0"))
	key.Write([]byte{0})
	key.Write([]byte("platform"))
	key.Write([]byte{0})
	key.Write([]byte(parts[4]))
	key.Write([]byte{0})
	return snap.HotplugKey(fmt.Sprintf("uio:%x", key.Sum(nil))), nil
}

func (iface *uioInterface) HandledByGadget(di *hotplug.HotplugDeviceInfo, slot *snap.SlotInfo) bool {
	var path string
	if err := slot.Attr("path", &path); err != nil {
		return false
	}
	return di.DeviceName() == path
}

func init() {
	registerIface(&uioInterface{})
}
