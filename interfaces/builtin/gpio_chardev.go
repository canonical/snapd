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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// TODO: Snapd should validate the correctness of slot declarations
// (i.e. lines across slots are unique) when installing the gadget
// snap e.g. when validating snap.yaml

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

var gpioChardevConnectedSlotKmod = []string{
	"gpio-aggregator",
}

type gpioChardevInterface struct {
	commonInterface
}

func validateSourceChips(sourceChip []string) error {
	if len(sourceChip) == 0 {
		return errors.New(`"source-chip" must contain at least one chip`)
	}
	exists := make(map[string]bool)
	for _, chip := range sourceChip {
		if chip == "" {
			return errors.New(`chip in "source-chip" cannot be empty`)
		}
		if chip != strings.TrimSpace(chip) {
			return fmt.Errorf(`chip in "source-chip" cannot contain leading or trailing white space, found %q`, chip)
		}
		if exists[chip] {
			return fmt.Errorf(`"source-chip" cannot contain duplicate chip names, found %q`, chip)
		}
		exists[chip] = true
	}
	return nil
}

// XXX: What should be the limit on max range.
const maxLinesCount = 65536

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

func (iface *gpioChardevInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	// gpio-chardev is hidden behind an experimental feature flag until kernel
	// improvements for the gpio-aggregator interface lands.
	// https://lore.kernel.org/all/20250203031213.399914-1-koichiro.den@canonical.com
	if !features.GPIOChardevInterface.IsEnabled() {
		_, flag := features.GPIOChardevInterface.ConfigOption()
		return fmt.Errorf("gpio-chardev interface requires the %q flag to be set", flag)
	}
	return nil
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

	serviceSuffix := fmt.Sprintf("gpio-chardev-%s", slotName)
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		// snap-gpio-helper export-chardev "<chip-labels>" "<lines>" "<gadget>" "<slot-name>"
		ExecStart: fmt.Sprintf("/usr/lib/snapd/snap-gpio-helper export-chardev %q %q %q %q", strings.Join(sourceChip, ","), lines, snapName, slotName),
		// snap-gpio-helper unexport-chardev "<chip-labels>" "<lines>" "<gadget>" "<slot-name>"
		ExecStop: fmt.Sprintf("/usr/lib/snapd/snap-gpio-helper unexport-chardev %q %q %q %q", strings.Join(sourceChip, ","), lines, snapName, slotName),
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
	plugName := slot.Name()
	plugSnapName := plug.Snap().InstanceName()

	target := fmt.Sprintf("/dev/snap/gpio-chardev/%s/%s", slotSnapName, slotName)
	symlink := fmt.Sprintf("/dev/snap/gpio-chardev/%s/%s", plugSnapName, plugName)

	// Create symlink pointing to exported virtual slot device.
	serviceSuffix := fmt.Sprintf("gpio-chardev-%s", plugName)
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		ExecStart:       fmt.Sprintf("/bin/sh -c 'mkdir -p %q && ln -s %q %q'", filepath.Dir(symlink), target, symlink),
		ExecStop:        fmt.Sprintf("/bin/sh -c 'rm -f %q'", symlink),
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
	snippet += fmt.Sprintf("/dev/snap/gpio-chardev/%s/ r,\n", plugSnapName)
	snippet += fmt.Sprintf("/dev/snap/gpio-chardev/%s/* r,", plugSnapName)
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
			connectedSlotKModModules: gpioChardevConnectedSlotKmod,
		},
	})
}
