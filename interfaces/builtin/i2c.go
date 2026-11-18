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
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const i2cSummary = `allows access to specific I2C controller`

const i2cBaseDeclarationSlots = `
  i2c:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
`

const i2cConnectedPlugAppArmorPath = `
# Description: Can access I2C controller

%s rw,
`

const i2cConnectedPlugAppArmorSysfsName = `
# Description: Can access I2C sysfs name

/sys/bus/i2c/devices/%s/** rw,
`

// The type for i2c interface
type i2cInterface struct{}

// Getter for the name of the i2c interface
func (iface *i2cInterface) Name() string {
	return "i2c"
}

func (iface *i2cInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              i2cSummary,
		BaseDeclarationSlots: i2cBaseDeclarationSlots,
	}
}

func (iface *i2cInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed i2c device nodes. It is gonna be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var i2cControlDeviceNodePattern = regexp.MustCompile("^/dev/i2c-[0-9]+$")

// Pattern to match allowed i2c sysfs names.
var i2cValidSysfsName = regexp.MustCompile("^[a-zA-Z0-9_-]+$")

// Check validity of the defined slot
func (iface *i2cInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	sysfsName, ok := slot.Attrs["sysfs-name"].(string)
	if ok {
		if !i2cValidSysfsName.MatchString(sysfsName) {
			return fmt.Errorf("%s sysfs-name attribute must be a valid sysfs-name", iface.Name())
		}
		if _, ok := slot.Attrs["path"].(string); ok {
			return fmt.Errorf("%s slot can only use path or sysfs-name", iface.Name())
		}
		return nil
	}

	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path or sysfs-name attribute", iface.Name())
	}
	// XXX: this interface feeds the cleaned path into the regex and is
	// left unchanged here for historical reasons. New interfaces (eg,
	// like raw-volume) should instead use verifySlotPathAttribute() which
	// performs additional verification.
	path = filepath.Clean(path)

	if !i2cControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

func (iface *i2cInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {

	// check if sysfsName is set and if so stop after that
	var sysfsName string
	if err := slot.Attr("sysfs-name", &sysfsName); err == nil {
		spec.AddSnippet(fmt.Sprintf(i2cConnectedPlugAppArmorSysfsName, sysfsName))
		return nil
	}

	// do path if sysfsName is not set (they can't be set both)
	var path string
	if err := slot.Attr("path", &path); err != nil {
		return nil
	}

	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf(i2cConnectedPlugAppArmorPath, cleanedPath))
	// Use parametric snippets to avoid parser slowdown.
	spec.AddParametricSnippet([]string{
		"/sys/devices/platform/{*,**.i2c}/i2c-" /* ###PARAM### */, "/** rw,  # Add any condensed parametric rules",
	}, strings.TrimPrefix(path, "/dev/i2c-"))
	return nil
}

func (iface *i2cInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var path string
	if err := slot.Attr("path", &path); err != nil {
		return nil
	}
	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *i2cInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&i2cInterface{})
}
