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
	"github.com/snapcore/snapd/snap"
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

var spiDevPattern = regexp.MustCompile(`^/dev/spidev[0-9]+\.[0-9]+$`)

func (iface *spiInterface) path(slotRef *interfaces.SlotRef, attrs interfaces.Attrer) (string, error) {
	var path string
	if err := attrs.Attr("path", &path); err != nil || path == "" {
		return "", fmt.Errorf("slot %q must have a path attribute", slotRef)
	}
	// XXX: this interface feeds the cleaned path into the regex and is
	// left unchanged here for historical reasons. New interfaces (eg,
	// like raw-volume) should instead use verifySlotPathAttribute() which
	// performs additional verification.
	path = filepath.Clean(path)
	if !spiDevPattern.MatchString(path) {
		return "", fmt.Errorf("%q is not a valid SPI device", path)
	}
	return path, nil
}

func (iface *spiInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_, err := iface.path(&interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}, slot)
	return err
}

func (iface *spiInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.AddSnippet(fmt.Sprintf("%s rw,", path))
	// Use parametric snippets to avoid parser slowdown.
	spec.AddParametricSnippet([]string{
		"/sys/devices/platform/**/**.spi/**/spidev" /* ###PARAM### */, "/** rw,  # Add any condensed parametric rules",
	}, strings.TrimPrefix(path, "/dev/spidev"))
	return nil
}

func (iface *spiInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, err := iface.path(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *spiInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&spiInterface{})
}
