// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/sandbox/gpio"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// TODO: Snapd should validate the correctness of slot declarations
// (i.e. lines across slots are unique) when installing the gadget
// snap e.g. when validating snap.yaml

// The interface operates as follows:
//   - uses snap-gpio-helper to set up a virtual GPIO device exposing specific
//     lines defined in the slot as character device node at
//     /dev/snap/gpio-chardev/<slot-snap>/<slot-name>
//   - sets up a symlink at /dev/snap/gpio-chardev/<plug-snap>/<plug-name>
//     pointing at the virtual slot device
//   - slot devices are given a specific udev tag, which consumer rules match on
//
// https://docs.kernel.org/userspace-api/gpio/chardev.html
// https://docs.kernel.org/admin-guide/gpio/gpio-aggregator.html
const gpioChardevSummary = `allows access to specific GPIO chardev lines`

const gpioChardevBaseDeclarationSlots = `
  gpio-chardev:
    allow-installation:
      slot-snap-type:
        - gadget
    deny-auto-connection: true
`

var gpioChardevPermanentSlotKmod = []string{
	"gpio-aggregator",
}

var gpioChardevPlugServiceSnippets = []interfaces.PlugServicesSnippet{
	interfaces.PlugServicesUnitSectionSnippet(`After=snapd.gpio-chardev-setup.target`),
	interfaces.PlugServicesUnitSectionSnippet(`Wants=snapd.gpio-chardev-setup.target`),
}

type gpioChardevInterface struct {
	commonInterface
}

func validateSourceChips(sourceChip []string) error {
	if len(sourceChip) == 0 {
		return errors.New(`cannot be empty`)
	}
	exists := make(map[string]bool, len(sourceChip))
	for _, chip := range sourceChip {
		if chip == "" {
			return errors.New(`chip cannot be empty`)
		}
		if chip != strings.TrimSpace(chip) {
			return fmt.Errorf(`chip cannot contain leading or trailing white space, found %q`, chip)
		}
		if exists[chip] {
			return fmt.Errorf(`cannot contain duplicate chip names, found %q`, chip)
		}
		exists[chip] = true
	}
	return nil
}

const maxLinesCount = 512

// BeforePrepareSlot checks validity of the defined slot.
func (iface *gpioChardevInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	var sourceChip []string
	// "source-chip" attribute is mandatory.
	if err := slot.Attr("source-chip", &sourceChip); err != nil {
		return err
	}
	if err := validateSourceChips(sourceChip); err != nil {
		return fmt.Errorf(`invalid "source-chip" attribute: %w`, err)
	}

	var lines string
	// "lines" attribute is mandatory.
	if err := slot.Attr("lines", &lines); err != nil {
		return err
	}
	r, err := strutil.ParseRange(lines)
	if err != nil {
		return fmt.Errorf(`invalid "lines" attribute: %w`, err)
	}
	// Check that range is not unrealistically large.
	if r.Size() > maxLinesCount {
		return fmt.Errorf(`invalid "lines" attribute: range size cannot exceed %d, found %d`, maxLinesCount, r.Size())
	}

	return nil
}

var gpioCheckConfigfsSupport = gpio.CheckConfigfsSupport

func (iface *gpioChardevInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	return gpioCheckConfigfsSupport()
}

func (iface *gpioChardevInterface) SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var sourceChip []string
	if err := slot.Attr("source-chip", &sourceChip); err != nil {
		return err
	}
	var lines string
	if err := slot.Attr("lines", &lines); err != nil {
		return err
	}
	snapName := slot.Snap().InstanceName()
	slotName := slot.Name()

	helperPath := filepath.Join(dirs.DistroLibExecDir, "snap-gpio-helper")
	serviceSuffix := fmt.Sprintf("gpio-chardev-%s", slotName)
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		// snap-gpio-helper export-chardev "<chip-labels>" "<lines>" "<gadget>" "<slot-name>"
		ExecStart: fmt.Sprintf("%s export-chardev %q %q %q %q",
			helperPath, strings.Join(sourceChip, ","), lines, snapName, slotName),
		// snap-gpio-helper unexport-chardev "<chip-labels>" "<lines>" "<gadget>" "<slot-name>"
		ExecStop: fmt.Sprintf("%s unexport-chardev %q %q %q %q",
			helperPath, strings.Join(sourceChip, ","), lines, snapName, slotName),
		// snapd.gpio-chardev-setup.target is used for synchronization of
		// app services and virtual device setup during boot.
		WantedBy: "snapd.gpio-chardev-setup.target",
		Before:   "snapd.gpio-chardev-setup.target",
	}
	return spec.AddService(serviceSuffix, service)
}

func (iface *gpioChardevInterface) SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	slotName := slot.Name()
	slotSnapName := slot.Snap().InstanceName()
	plugName := plug.Name()
	plugSnapName := plug.Snap().InstanceName()

	target := gpio.SnapChardevPath(slotSnapName, slotName)
	symlink := gpio.SnapChardevPath(plugSnapName, plugName)

	// Create symlink pointing to exported virtual slot device.
	serviceSuffix := fmt.Sprintf("gpio-chardev-%s", plugName)
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		ExecStart:       fmt.Sprintf("/bin/sh -c 'mkdir -p %q && ln -s %q %q'", filepath.Dir(symlink), target, symlink),
		ExecStop:        fmt.Sprintf("/bin/rm -f %q", symlink),
		WantedBy:        "snapd.gpio-chardev-setup.target",
		Before:          "snapd.gpio-chardev-setup.target",
	}
	return spec.AddService(serviceSuffix, service)
}

func (iface *gpioChardevInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	slotSnapName := slot.Snap().InstanceName()
	plugSnapName := plug.Snap().InstanceName()
	snippet := "# Allow access to exported gpio chardev lines\n"
	// Allow access to exported virtual slot device.
	snippet += fmt.Sprintf("/dev/snap/gpio-chardev/%s/%s rwk,\n", slotSnapName, slot.Name())
	// Allow access to plug-side symlink to exported virtual slot device.
	snippet += fmt.Sprintf("/dev/snap/gpio-chardev/%s/{,*} r,\n", plugSnapName)
	spec.AddSnippet(snippet)

	return nil
}

func (iface *gpioChardevInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Match exported virtual device on special udev tag set by snap-gpio-helper.
	spec.TagDevice(fmt.Sprintf(`TAG=="snap_%s_interface_gpio_chardev_%s"`, slot.Snap().InstanceName(), slot.Name()))
	return nil
}

func init() {
	registerIface(&gpioChardevInterface{
		commonInterface{
			name:                     "gpio-chardev",
			summary:                  gpioChardevSummary,
			baseDeclarationSlots:     gpioChardevBaseDeclarationSlots,
			permanentSlotKModModules: gpioChardevPermanentSlotKmod,
			serviceSnippets:          gpioChardevPlugServiceSnippets,
		},
	})
}
