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
    deny-auto-connection: true
  bluez:
    deny-auto-connection: true
  bool-file:
    deny-auto-connection: true
  camera:
    deny-auto-connection: true
  content:
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
  cups-control:
    deny-auto-connection: true
  docker:
    deny-auto-connection: true
  docker-support:
    deny-auto-connection: true
  firewall-control:
    deny-auto-connection: true
  fuse-support:
    deny-auto-connection: true
  fwupd:
    deny-auto-connection: true
  gpio:
    deny-auto-connection: true
  hardware-observe:
    deny-auto-connection: true
  hidraw:
    deny-auto-connection: true
  home:
    deny-auto-connection: false
  kernel-module-control:
    deny-auto-connection: true
  libvirt:
    deny-auto-connection: true
  locale-control:
    deny-auto-connection: true
  location-control:
    deny-auto-connection: true
  location-observe:
    deny-auto-connection: true
  log-observe:
    deny-auto-connection: true
  lxd-support:
    allow-auto-connection:
      plug-publisher-id:
        - canonical
      plug-snap-id:
        - J60k4JY0HppjwOjW8dZdYc8obXKxujRu
  modem-manager:
    deny-auto-connection: true
  mount-observe:
    deny-auto-connection: true
  mpris:
    deny-auto-connection: true
  network-control:
    deny-auto-connection: true
  network-manager:
    deny-auto-connection: true
  network-observe:
    deny-auto-connection: true
  network-setup-observe:
    deny-auto-connection: true
  ppp:
    deny-auto-connection: true
  process-control:
    deny-auto-connection: true
  removable-media:
    deny-auto-connection: true
  serial-port:
    deny-auto-connection: true
  snapd-control:
    deny-auto-connection: false
  system-observe:
    deny-auto-connection: true
  system-trace:
    deny-auto-connection: true
  time-control:
    deny-auto-connection: true
  timeserver-control:
    deny-auto-connection: true
  timezone-control:
    deny-auto-connection: true
  tpm:
    deny-auto-connection: true
  udisks2:
    deny-auto-connection: true
`

/* TODO:

 * we want snap-control to be deny-auto-connection once we can set snap decls

 */

func init() {
	err := asserts.InitBuiltinBaseDeclaration([]byte(baseDeclarationHeaders))
	if err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
