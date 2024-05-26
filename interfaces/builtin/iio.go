// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const iioSummary = `allows access to a specific IIO device`

const iioBaseDeclarationSlots = `
  iio:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
`

const iioConnectedPlugAppArmor = `
# Description: Give access to a specific IIO device on the system.

###IIO_DEVICE_PATH### rw,
/sys/bus/iio/devices/###IIO_DEVICE_NAME###/ r,
/sys/bus/iio/devices/###IIO_DEVICE_NAME###/** rwk,
`

// The type for iio interface
type iioInterface struct{}

// Getter for the name of the iio interface
func (iface *iioInterface) Name() string {
	return "iio"
}

func (iface *iioInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              iioSummary,
		BaseDeclarationSlots: iioBaseDeclarationSlots,
	}
}

func (iface *iioInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed iio device nodes. It is going to be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var iioControlDeviceNodePattern = regexp.MustCompile("^/dev/iio:device[0-9]+$")

// Check validity of the defined slot
func (iface *iioInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path attribute", iface.Name())
	}

	// XXX: this interface feeds the cleaned path into the regex and is
	// left unchanged here for historical reasons. New interfaces (eg,
	// like raw-volume) should instead use verifySlotPathAttribute() which
	// performs additional verification.
	path = filepath.Clean(path)

	if !iioControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

func (iface *iioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var path string
	mylog.Check(slot.Attr("path", &path))

	cleanedPath := filepath.Clean(path)
	snippet := strings.Replace(iioConnectedPlugAppArmor, "###IIO_DEVICE_PATH###", cleanedPath, -1)

	// The path is already verified against a regular expression
	// in BeforePrepareSlot so we can rely on its structure here and
	// safely strip the '/dev/' prefix to get the actual name of
	// the IIO device.
	deviceName := strings.TrimPrefix(path, "/dev/")
	snippet = strings.Replace(snippet, "###IIO_DEVICE_NAME###", deviceName, -1)

	// Add a snippet for various device specific rules, except for sysfs write
	// access that are specialized below.
	spec.AddSnippet(snippet)

	// Because all deviceName values have the prefix of "iio:device" enforced
	// by the sanitization logic above, we can trim that prefix and provide a
	// shorter expansion expression.
	deviceNum := strings.TrimPrefix(deviceName, "iio:device")

	// Use parametric snippets to avoid parser slowdown.
	spec.AddParametricSnippet([]string{
		"/sys/devices/**/iio:device" /* ###PARAM### */, "/** rwk,  # Add any condensed parametric rules",
	}, deviceNum)
	// For consistency, not an efficiency problem.
	spec.AddParametricSnippet([]string{
		"/sys/devices/**/iio:device" /* ###PARAM### */, "/ r,  # Add any condensed parametric rules",
	}, deviceNum)

	return nil
}

func (iface *iioInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var path string
	mylog.Check(slot.Attr("path", &path))

	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *iioInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&iioInterface{})
}
