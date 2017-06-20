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

package policy

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
)

// The headers of the builtin base-declaration describing the default
// interface policies for all snaps. The base declaration focuses on the slot
// side for almost all interfaces. Importantly, items are not merged between
// the slots and plugs or between the base declaration and snap declaration
// for a particular type of rule. This means that if you specify an
// installation rule for both slots and plugs in the base declaration, only
// the plugs side is used (plugs is preferred over slots).
//
// The interfaces listed in the base declaration can be broadly categorized
// into:
//
// - manually connected implicit slots (eg, bluetooth-control)
// - auto-connected implicit slots (eg, network)
// - manually connected app-provided slots (eg, bluez)
// - auto-connected app-provided slots (eg, mir)
//
// such that they will follow this pattern:
//
//   slots:
//     manual-connected-implicit-slot:
//       allow-installation:
//         slot-snap-type:
//           - core                     # implicit slot
//       deny-auto-connection: true     # force manual connect
//
//     auto-connected-implicit-slot:
//       allow-installation:
//         slot-snap-type:
//           - core                     # implicit slot
//       allow-auto-connection: true    # allow auto-connect
//
//     manual-connected-provided-slot:
//       allow-installation:
//         slot-snap-type:
//           - app                      # app provided slot
//       deny-connection: true          # require allow-connection in snap decl
//       deny-auto-connection: true     # force manual connect
//
//     auto-connected-provided-slot:
//       allow-installation:
//         slot-snap-type:
//           - app                      # app provided slot
//       deny-connection: true          # require allow-connection in snap decl
//
// App-provided slots use 'deny-connection: true' since slot implementations
// require privileged access to the system and the snap must be trusted. In
// this manner a snap declaration is required to override the base declaration
// to allow connections with the app-provided slot.
//
// Slots dealing with hardware typically will specify 'gadget' and 'core' as
// the slot-snap-type (eg, serial-port). Eg:
//
//   slots:
//     manual-connected-hw-slot:
//       allow-installation:
//         slot-snap-type:
//           - core
//           - gadget
//       deny-auto-connection: true
//
// So called super-privileged slot implementations should also be disallowed
// installation on a system and a snap declaration is required to override the
// base declaration to allow installation (eg, docker). Eg:
//
//   slots:
//     manual-connected-super-privileged-slot:
//       allow-installation: false
//       deny-connection: true
//       deny-auto-connection: true
//
// Like super-privileged slot implementation, super-privileged plugs should
// also be disallowed installation on a system and a snap declaration is
// required to override the base declaration to allow installation (eg,
// kernel-module-control). Eg:
//
//   plugs:
//     manual-connected-super-privileged-plug:
//       allow-installation: false
//       deny-auto-connection: true
//   (remember this overrides slot side rules)
//
// Some interfaces have policy that is meant to be used with slot
// implementations and on classic images. Since the slot implementation is
// privileged, we require a snap declaration to be used for app-provided slot
// implementations on non-classic systems (eg, network-manager). Eg:
//
//   slots:
//     classic-or-not-slot:
//       allow-installation:
//         slot-snap-type:
//           - app
//           - core
//       deny-auto-connection: true
//       deny-connection:
//         on-classic: false
//
// Some interfaces have policy that is only used with implicit slots on
// classic and should be autoconnected only there (eg, home). Eg:
//
//   slots:
//     implicit-classic-slot:
//       allow-installation:
//         slot-snap-type:
//           - core
//     deny-auto-connection:
//       on-classic: false
//

const baseDeclarationHeader = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
`

const baseDeclarationPlugs = `
plugs:
  lxd-support:
    allow-installation: false
    deny-auto-connection: true
  snapd-control:
    allow-installation: false
    deny-auto-connection: true
  unity8:
    allow-installation: false
`

const baseDeclarationSlots = `
slots:
  lxd-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  maliit:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
  media-hub:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
  mir:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  modem-manager:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
  mount-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  mpris:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection:
      slot-attributes:
        name: .+
    deny-auto-connection: true
  netlink-audit:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  netlink-connector:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network:
    allow-installation:
      slot-snap-type:
        - core
  network-bind:
    allow-installation:
      slot-snap-type:
        - core
  network-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network-manager:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
  network-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network-setup-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network-setup-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network-status:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  ofono:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
  online-accounts-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  opengl:
    allow-installation:
      slot-snap-type:
        - core
  openvswitch:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  openvswitch-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  optical-drive:
    allow-installation:
      slot-snap-type:
        - core
  physical-memory-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  physical-memory-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  ppp:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  process-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  pulseaudio:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
  raw-usb:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  removable-media:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  screen-inhibit-control:
    allow-installation:
      slot-snap-type:
        - core
  serial-port:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
  shutdown:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  snapd-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  storage-framework-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
  system-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  system-trace:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  thumbnailer-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection: true
  time-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  timeserver-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  timezone-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  tpm:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  udisks2:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
  uhid:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  unity7:
    allow-installation:
      slot-snap-type:
        - core
  unity8:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  unity8-calendar:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection: true
  unity8-contacts:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection: true
  ubuntu-download-manager:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  upower-observe:
    allow-installation:
      slot-snap-type:
        - core
        - app
    deny-connection:
      on-classic: false
  x11:
    allow-installation:
      slot-snap-type:
        - core
`

func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\n")
}

func composeBaseDeclaration(ifaces []interfaces.Interface) ([]byte, error) {
	var buf bytes.Buffer
	// Trim newlines at the end of the string. All the elements may have
	// spurious trailing newlines. All elements start with a leading newline.
	// We don't want any blanks as that would no longer parse.
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationHeader)); err != nil {
		return nil, err
	}
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationPlugs)); err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		plugPolicy := interfaces.IfaceMetaData(iface).BaseDeclarationPlugs
		if _, err := buf.WriteString(trimTrailingNewline(plugPolicy)); err != nil {
			return nil, err
		}
	}
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationSlots)); err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		slotPolicy := interfaces.IfaceMetaData(iface).BaseDeclarationSlots
		if _, err := buf.WriteString(trimTrailingNewline(slotPolicy)); err != nil {
			return nil, err
		}
	}
	if _, err := buf.WriteRune('\n'); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func init() {
	decl, err := composeBaseDeclaration(builtin.Interfaces())
	if err != nil {
		panic(fmt.Sprintf("cannot compose base-declaration: %v", err))
	}
	if err := asserts.InitBuiltinBaseDeclaration(decl); err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
