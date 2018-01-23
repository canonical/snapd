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
	"os"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const softwareWatchdogSummary = `allows use of systemd service watchdog`

const softwareWatchdogBaseDeclarationSlots = `
  software-watchdog:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const softwareWatchdogConnectedPlugAppArmorTemplate = `
# Allow sending notification messages to systemd through the notify socket
{{notify-socket}} w,
`

type softwareWatchdogInterface struct {
	commonInterface
}

var osGetenv = os.Getenv

func (iface *softwareWatchdogInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	notifySocket := osGetenv("NOTIFY_SOCKET")
	if notifySocket == "" {
		notifySocket = "/run/systemd/notify"
	}
	snippet := strings.Replace(softwareWatchdogConnectedPlugAppArmorTemplate,
		"{{notify-socket}}", notifySocket, 1)
	spec.AddSnippet(snippet)
	return nil
}

func init() {
	registerIface(&softwareWatchdogInterface{commonInterface: commonInterface{
		name:                 "software-watchdog",
		summary:              softwareWatchdogSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: softwareWatchdogBaseDeclarationSlots,
		// implemented by AppArmorConnectedPlug()
		connectedPlugAppArmor: "",
		reservedForOS:         true,
	}})
}
