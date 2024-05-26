// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

// https://www.kernel.org/doc/Documentation/pwm.txt
const pwmSummary = `allows access to specific PWM channel`

const pwmBaseDeclarationSlots = `
  pwm:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

var pwmSysfsPwmChipBase = "/sys/class/pwm/pwmchip%d"

// pwmInterface type
type pwmInterface struct {
	commonInterface
}

// BeforePrepareSlot checks the slot definition is valid
func (iface *pwmInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// must have a PWM channel
	channel, ok := slot.Attrs["channel"]
	if !ok {
		return fmt.Errorf("pwm slot must have a channel attribute")
	}

	// valid values of channel
	if _, ok := channel.(int64); !ok {
		return fmt.Errorf("pwm slot channel attribute must be an int")
	}

	// must have a PWM chip number
	chipNum, ok := slot.Attrs["chip-number"]
	if !ok {
		return fmt.Errorf("pwm slot must have a chip-number attribute")
	}

	// valid values of chip number
	if _, ok := chipNum.(int64); !ok {
		return fmt.Errorf("pwm slot chip-number attribute must be an int")
	}

	// slot is good
	return nil
}

func (iface *pwmInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var chipNum int64
	mylog.Check(slot.Attr("chip-number", &chipNum))

	var channel int64
	mylog.Check(slot.Attr("channel", &channel))

	path := fmt.Sprintf(pwmSysfsPwmChipBase, chipNum)
	// Entries in /sys/class/pwm for PWM chips are just symlinks
	// to their correct device part in the sysfs tree. Given AppArmor
	// requires symlinks to be dereferenced, evaluate the PWM
	// path and add the correct absolute path to the AppArmor snippet.
	dereferencedPath := mylog.Check2(evalSymlinks(path))
	if err != nil && os.IsNotExist(err) {
		// If the specific pwm is not available there is no point
		// exporting it, we should also not fail because this
		// will block snapd updates (LP: 1866424)
		logger.Noticef("cannot find not existing pwm chipbase %s", path)
		return nil
	}

	spec.AddSnippet(fmt.Sprintf("%s/pwm%d/* rwk,", dereferencedPath, channel))
	return nil
}

func (iface *pwmInterface) SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var chipNum int64
	mylog.Check(slot.Attr("chip-number", &chipNum))

	var channel int64
	mylog.Check(slot.Attr("channel", &channel))

	serviceSuffix := fmt.Sprintf("pwmchip%d-pwm%d", chipNum, channel)
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		ExecStart:       fmt.Sprintf("/bin/sh -c 'test -e /sys/class/pwm/pwmchip%[1]d/pwm%[2]d || echo %[2]d > /sys/class/pwm/pwmchip%[1]d/export'", chipNum, channel),
		ExecStop:        fmt.Sprintf("/bin/sh -c 'test ! -e /sys/class/pwm/pwmchip%[1]d/pwm%[2]d || echo %[2]d > /sys/class/pwm/pwmchip%[1]d/unexport'", chipNum, channel),
	}
	return spec.AddService(serviceSuffix, service)
}

func (iface *pwmInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&pwmInterface{commonInterface{
		name:                 "pwm",
		summary:              pwmSummary,
		baseDeclarationSlots: pwmBaseDeclarationSlots,
	}})
}
