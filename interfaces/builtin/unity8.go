// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces"
)

var unity8ConnectedPlugAppArmor = []byte(`
# Description: Can access unity8 desktop services
# Usage: common

  #include <abstractions/dbus-session-strict>
  #include <abstractions/fonts>

  # qmlscene (a common client) wants this
  network netlink raw,

  # Accessibility
  #include <abstractions/dbus-accessibility-strict>
  dbus (send)
       bus=session
       path=/org/a11y/bus
       interface=org.a11y.Bus
       member=GetAddress
       peer=(name=org.a11y.Bus,label=unconfined),
  dbus (receive, send)
       bus=accessibility
       path=/org/a11y/atspi/**
       peer=(label=unconfined),

  # OSK (on screen keyboard)
  dbus (send)
       bus=session
       path=/org/maliit/server/address
       interface=org.freedesktop.DBus.Properties
       member=Get
       peer=(name=org.maliit.server,label=unconfined),
  unix (connect, receive, send)
       type=stream
       peer=(addr=@/tmp/maliit-server/dbus-*),

  # Sensors
  dbus (receive, send)
       bus=session
       path=/com/canonical/usensord/haptic
       peer=(name=com.canonical.usensord,label=unconfined),

  # URL dispatcher. All apps can call this since:
  # a) the dispatched application is launched out of process and not
  #    controllable except via the specified URL
  # b) the list of url types is strictly controlled
  # c) the dispatched application will launch in the foreground over the
  #    confined app
  dbus (send)
       bus=session
       path=/com/canonical/URLDispatcher
       interface=com.canonical.URLDispatcher
       member=DispatchURL
       peer=(name=com.canonical.URLDispatcher,label=unconfined),

  # This is needed when the app is already running and needs to be passed in
  # a URL to open. This is most often used with content-hub providers and
  # url-dispatcher, but is actually supported by Qt generally (though because
  # we don't allow the send a malicious app can't send this to another app).
  dbus (receive)
       bus=session
       path=/@{PROFILE_DBUS}
       interface=org.freedesktop.Application
       member=Open
       peer=(label=unconfined),

  # Access to unity launcher (e.g. app counter, progress, alert)
  dbus (receive, send)
       bus=session
       path=/com/canonical/Unity/Launcher/@{PROFILE_DBUS}
       peer=(name=com.canonical.Unity.Launcher,label=unconfined),

  # Clipboard
  dbus (send)
       bus=session
       interface=com.ubuntu.content.dbus.Service
       path=/
       member={CreatePaste,GetLatestPasteData,GetPasteData,PasteFormats}
       peer=(name=com.ubuntu.content.dbus.Service,label=unconfined),
  dbus (receive)
       bus=session
       interface=com.ubuntu.content.dbus.Service
       path=/
       member=PasteFormatsChanged
       peer=(name=com.ubuntu.content.dbus.Service,label=unconfined),

  # Lttng tracing is very noisy and should not be allowed by confined apps.
  # Can safely deny. LP: #1260491
  deny /{,var/}{dev,run}/shm/lttng-ust-* r,
`)

var unity8ConnectedPlugSecComp = []byte(`
bind
sched_setscheduler
sendto
shutdown
`)

type Unity8Interface struct{}

func (iface *Unity8Interface) Name() string {
	return "unity8"
}

func (iface *Unity8Interface) String() string {
	return iface.Name()
}

func (iface *Unity8Interface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return unity8ConnectedPlugAppArmor, nil
	case interfaces.SecuritySecComp:
		return unity8ConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *Unity8Interface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

func (iface *Unity8Interface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

func (iface *Unity8Interface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}
