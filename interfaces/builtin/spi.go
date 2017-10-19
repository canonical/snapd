// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
)

const spiSummary = `allows access to specific spi controller`

const spiBaseDeclarationSlots = `
  spi:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

type spiInterface struct{}

func (iface *spiInterface) Name() string {
	return "spi"
}

func (iface *spiInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              spiSummary,
		BaseDeclarationSlots: spiBaseDeclarationSlots,
	}
}

var spiDevPattern = regexp.MustCompile("^/dev/spidev[0-9].[0-9]+$")

func (iface *spiInterface) path(attrs map[string]interface{}) (string, error) {
	path, ok := attrs["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("spi slot must have a path attribute")
	}
	path = filepath.Clean(path)
	if !spiDevPattern.MatchString(path) {
		return "", fmt.Errorf("%q is not a valid SPI device", path)
	}
	return path, nil
}

func (iface *spiInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if err := sanitizeSlotReservedForOSOrGadget(iface, slot); err != nil {
		return err
	}
	_, err := iface.path(slot.Attrs)
	return err
}

func (iface *spiInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Attrs)
	if err != nil {
		return nil
	}
	spec.AddSnippet(fmt.Sprintf("%s rw,", path))
	spec.AddSnippet(fmt.Sprintf("/sys/devices/platform/**/**.spi/**/%s/** rw,", strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *spiInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Attrs)
	if err != nil {
		return nil
	}
	for appName := range plug.Apps() {
		tag := udevSnapSecurityName(plug.Snap().Name(), appName)
		spec.AddSnippet(fmt.Sprintf(`KERNEL=="%s", TAG+="%s"`, strings.TrimPrefix(path, "/dev/"), tag))
	}
	return nil
}

func (iface *spiInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&spiInterface{})
}
