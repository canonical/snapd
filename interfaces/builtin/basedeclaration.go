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

	"github.com/snapcore/snapd/asserts"
)

// The headers of the builtin base-declaration describing the default
// interface policies for all snaps. The base declaration focuses on the slot
// side for almost all interfaces.
//
// The interfaces listed in the base declaration can be broadly categorized
// into:
//
// - manually connected implicit slots
// - auto-connected implicit slots
// - manually connected app-provided slots
// - auto-connected app-provided slots
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
// the slot-snap-type. Eg:
//
//   slots:
//     manual-connected-hw-slot:
//       allow-installation:
//         slot-snap-type:
//           - core
//           - gadget
//       deny-auto-connection: true
//
// So called super-privileged slot inmplementations should also be disallowed
// installation on a system and a snap declaration is required to override the
// base declaration to allow installation. Eg:
//
//   slots:
//     manual-connected-super-privileged-slot:
//       allow-installation: false
//       deny-connection: true
//       deny-auto-connection: true
//
// Like super-privileged slot implementation, super-privileged plugs should
// also be disalled installation on a system and a snap declaration is required
// to override the base declaration to allow installation. Eg:
//
//   plugs:
//     manual-connected-super-privileged-plug:
//       allow-installation: false
//       deny-connection: true
//       deny-auto-connection: true
//
//
// TODO: when on-classic is implemented
//
// Some interfaces have policy that is meant to be used with slot
// implementations and on classic images. Since the slot implementation is
// privileged, we require a snap declaration to be used for app-provided slot
// implementations on non-classic systems. Eg:
//
//   slots:
//     classic-or-not-slot:
//       deny-installation:
//         slot-snap-type:
//           - app
//         on-classic: false
//
// TODO: pulseaudio and network-manager should do this
//
// Some interfaces have policy that is only used with implicit slots on
// classic and should be autoconnected only there. Eg:
//
//   slots:
//     implicit-classic-slot:
//       allow-installation:
//         slot-snap-type:
//           - core
//     deny-auto-connection:
//       on-classic: false
//
// TODO: implicit classic slots should do this
//
const baseDeclarationHeaders = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
plugs:
  docker-support:
    allow-installation: false
    deny-connection: true
  kernel-module-control:
    allow-installation: false
    deny-connection: true
  lxd-support:
    allow-installation: false
    deny-connection: true
  snapd-control:
    allow-installation: false
    deny-connection: true
slots:
  bluetooth-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  bluez:
    deny-connection: true
    deny-auto-connection: true
  bool-file:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
  browser-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection:
      plug-attributes:
        allow-sandbox: true
  camera:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  content:
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
    allow-installation:
      slot-snap-type:
        - app
        - gadget
  cups-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  docker:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
  docker-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  firewall-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  fuse-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  fwupd:
    deny-connection: true
    deny-auto-connection: true
  gpio:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
  gsettings:
    allow-installation:
      slot-snap-type:
        - core
  hardware-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  hidraw:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
  home:
    allow-installation:
      slot-snap-type:
        - core
  kernel-module-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  libvirt:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  locale-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  location-control:
    deny-connection: true
    deny-auto-connection: true
  location-observe:
    deny-connection: true
    deny-auto-connection: true
  log-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  lxd-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  mir:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
  modem-manager:
    deny-connection: true
    deny-auto-connection: true
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
    deny-connection: true
    deny-auto-connection: true
  network-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  network-setup-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  opengl:
    allow-installation:
      slot-snap-type:
        - core
  optical-drive:
    allow-installation:
      slot-snap-type:
        - core
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
    deny-connection: true
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
  snapd-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: false
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
    deny-connection: true
    deny-auto-connection: true
  unity7:
    allow-installation:
      slot-snap-type:
        - core
  upower-observe:
    allow-installation:
      slot-snap-type:
        - core
  x11:
    allow-installation:
      slot-snap-type:
        - core
`

func init() {
	err := asserts.InitBuiltinBaseDeclaration([]byte(baseDeclarationHeaders))
	if err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
