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

// the headers of the builtin base-declaration describing the default
// interface policies for all snaps.
const baseDeclarationHeaders = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
slots:
  bluetooth-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  bluez:
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
  dcdbas-control:
    allow-installation:
      slot-snap-type:
        - gadget
    deny-auto-connection: true
  docker:
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
    deny-auto-connection: false
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
    deny-auto-connection: true
  location-observe:
    deny-auto-connection: true
  log-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
  lxd-support:
    allow-auto-connection:
      plug-publisher-id:
        - canonical
      plug-snap-id:
        - J60k4JY0HppjwOjW8dZdYc8obXKxujRu
    allow-installation:
      slot-snap-type:
        - core
  mir:
    allow-installation:
      slot-snap-type:
        - app
  modem-manager:
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
    allow-installation:
      slot-snap-type:
        - core
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

/* TODO:

* once we have snap-declaration edit support:
    - we want snapd-control to be deny-auto-connection
    - slot side docker: false
    - plugs side *-installation rules
    - *-connection rules
    - no *-snap-id *-publisher-id constraints here

*/

func init() {
	err := asserts.InitBuiltinBaseDeclaration([]byte(baseDeclarationHeaders))
	if err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
