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
const baseDeclarationHeaders = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
plugs:
  classic-support:
    allow-installation: false
    deny-auto-connection: true
  core-support:
    allow-installation:
      plug-snap-type:
        - core
  docker-support:
    allow-installation: false
    deny-auto-connection: true
  kernel-module-control:
    allow-installation: false
    deny-auto-connection: true
  lxd-support:
    allow-installation: false
    deny-auto-connection: true
  snapd-control:
    allow-installation: false
    deny-auto-connection: true
  unity8:
    allow-installation: false
slots:
  account-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  alsa:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  avahi-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  bluetooth-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  bluez:
    allow-installation:
      slot-snap-type:
        - app
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
  chroot:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  classic-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  content:
    allow-installation:
      slot-snap-type:
        - app
        - gadget
    allow-connection:
      plug-attributes:
        content: $SLOT(content)
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
      plug-attributes:
        content: $SLOT(content)
  core-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  cups-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  dbus:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection:
      slot-attributes:
        name: .+
    deny-auto-connection: true
  dcdbas-control:
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
    allow-installation:
      slot-snap-type:
        - app
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
    deny-auto-connection:
      on-classic: false
  i2c:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
  iio:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
  io-ports-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
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
  framebuffer:
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
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
  location-observe:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
  log-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  lxd:
    allow-installation: false
    deny-connection: true
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
  ofono:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
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
  thumbnailer:
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

func init() {
	err := asserts.InitBuiltinBaseDeclaration([]byte(baseDeclarationHeaders))
	if err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
