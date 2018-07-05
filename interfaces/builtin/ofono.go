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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const ofonoSummary = `allows operating as the ofono service`

const ofonoBaseDeclarationSlots = `
  ofono:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const ofonoPermanentSlotAppArmor = `
# Description: Allow operating as the ofono service. This gives privileged
# access to the system.

# to create ppp network interfaces
capability net_admin,

# To check present devices
/run/udev/data/+usb:* r,
/run/udev/data/+usb-serial:* r,
/run/udev/data/+pci:* r,
/run/udev/data/+platform:* r,
/run/udev/data/+pnp:* r,
/run/udev/data/c* r,
/run/udev/data/n* r,
/sys/bus/usb/devices/ r,
# FIXME snapd should be querying udev and adding the /sys and /run/udev accesses
# that are assigned to the snap, but we are not there yet.
/sys/bus/usb/devices/** r,

# To get current seat, used to know user preferences like default SIM in
# multi-SIM devices.
/run/systemd/seats/{,*} r,

# Access to modem ports
# FIXME snapd should be more dynamic to avoid conflicts between snaps trying to
# access same ports.
/dev/tty[^0-9]* rw,
/dev/cdc-* rw,
/dev/modem* rw,
/dev/dsp rw,
/dev/chnlat11 rw,
/dev/socket/rild* rw,
# ofono puts ppp on top of the tun device
/dev/net/tun rw,

network netlink raw,
network netlink dgram,
network bridge,
network inet,
network inet6,
network packet,
network bluetooth,

include <abstractions/nameservice>
/run/systemd/resolve/stub-resolv.conf r,

# DBus accesses
include <abstractions/dbus-strict>

# systemd-resolved (not yet included in nameservice abstraction)
#
# Allow access to the safe members of the systemd-resolved D-Bus API:
#
#   https://www.freedesktop.org/wiki/Software/systemd/resolved/
#
# This API may be used directly over the D-Bus system bus or it may be used
# indirectly via the nss-resolve plugin:
#
#   https://www.freedesktop.org/software/systemd/man/nss-resolve.html
#
dbus send
     bus=system
     path="/org/freedesktop/resolve1"
     interface="org.freedesktop.resolve1.Manager"
     member="Resolve{Address,Hostname,Record,Service}"
     peer=(name="org.freedesktop.resolve1"),

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={Request,Release}Name
    peer=(name=org.freedesktop.DBus, label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.ofono",

# Allow traffic to/from our path and interface with any method for unconfined
# clients to talk to our ofono services.
dbus (receive, send)
    bus=system
    path=/{,**}
    interface=org.ofono.*
    peer=(label=unconfined),
`

const ofonoConnectedSlotAppArmor = `
# Allow service to interact with connected clients

# Allow traffic to/from our interfaces. The path depends on the modem plugin,
# and is arbitrary.
dbus (receive, send)
    bus=system
    path=/{,**}
    interface=org.ofono.*
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const ofonoConnectedPlugAppArmor = `
# Description: Allow using Ofono service. This gives privileged access to the
# Ofono service.

#include <abstractions/dbus-strict>

# Allow all access to ofono services
dbus (receive, send)
    bus=system
    path=/{,**}
    interface=org.ofono.*
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service on non-classic (due to the path,
# allowing on classic would reveal too much for unconfined)
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const ofonoConnectedPlugAppArmorClassic = `
# Allow access to the unconfined ofono services on classic.
dbus (receive, send)
    bus=system
    path=/{,**}
    interface=org.ofono.*
    peer=(label=unconfined),

# Don't allow introspection since it reveals too much (path is not service
# specific for unconfined)
#dbus (send)
#    bus=system
#    path=/
#    interface=org.freedesktop.DBus.Introspectable
#    member=Introspect
#    peer=(label=unconfined),
`

const ofonoPermanentSlotSecComp = `
# Description: Allow operating as the ofono service. This gives privileged
# access to the system.

# Communicate with DBus, netlink, rild
accept
accept4
bind
listen
socket AF_NETLINK - NETLINK_ROUTE
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const ofonoPermanentSlotDBus = `
<!-- Comes from src/ofono.conf in sources -->

<policy user="root">
  <allow own="org.ofono"/>
  <allow send_destination="org.ofono"/>
  <allow send_interface="org.ofono.SimToolkitAgent"/>
  <allow send_interface="org.ofono.PushNotificationAgent"/>
  <allow send_interface="org.ofono.SmartMessagingAgent"/>
  <allow send_interface="org.ofono.PositioningRequestAgent"/>
  <allow send_interface="org.ofono.HandsfreeAudioAgent"/>
</policy>

<policy context="default">
  <deny send_destination="org.ofono"/>
  <!-- Additional restriction in next line (not in ofono.conf) -->
  <deny own="org.ofono"/>
</policy>
`

const ofonoPermanentSlotUDev = `
## Concatenation of all ofono udev rules (plugins/*.rules in ofono sources)
## Note that ofono uses this for very few modems and that in most cases it finds
## modems by checking directly in code udev events, so changes here will be rare

## plugins/ofono.rules
# do not edit this file, it will be overwritten on update

ACTION!="add|change", GOTO="ofono_end"

# ISI/Phonet drivers
SUBSYSTEM!="net", GOTO="ofono_isi_end"
ATTRS{type}!="820", GOTO="ofono_isi_end"
KERNELS=="gadget", GOTO="ofono_isi_end"

# Nokia N900 modem
SUBSYSTEMS=="hsi", ENV{OFONO_DRIVER}="n900", ENV{OFONO_ISI_ADDRESS}="108"
KERNEL=="phonet*", ENV{OFONO_DRIVER}="n900", ENV{OFONO_ISI_ADDRESS}="108"

# STE u8500
KERNEL=="shrm0", ENV{OFONO_DRIVER}="u8500"

LABEL="ofono_isi_end"

SUBSYSTEM!="usb", GOTO="ofono_end"
ENV{DEVTYPE}!="usb_device", GOTO="ofono_end"

# Ignore fake serial number
ATTRS{serial}=="1234567890ABCDEF", ENV{ID_SERIAL_SHORT}=""

# Nokia CDMA Device
ATTRS{idVendor}=="0421", ATTRS{idProduct}=="023e", ENV{OFONO_DRIVER}="nokiacdma"
ATTRS{idVendor}=="0421", ATTRS{idProduct}=="00b6", ENV{OFONO_DRIVER}="nokiacdma"

# Lenovo H5321gw 0bdb:1926
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1926", ENV{OFONO_DRIVER}="mbm"

LABEL="ofono_end"

## plugins/ofono-speedup.rules
# do not edit this file, it will be overwritten on update

ACTION!="add|change", GOTO="ofono_speedup_end"

SUBSYSTEM!="tty", GOTO="ofono_speedup_end"
KERNEL!="ttyUSB[0-9]*", GOTO="ofono_speedup_end"

# SpeedUp 7300
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9e00", ENV{ID_USB_INTERFACE_NUM}=="00", ENV{OFONO_LABEL}="modem"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9e00", ENV{ID_USB_INTERFACE_NUM}=="03", ENV{OFONO_LABEL}="aux"

# SpeedUp
ATTRS{idVendor}=="2020", ATTRS{idProduct}=="1005", ENV{ID_USB_INTERFACE_NUM}=="03", ENV{OFONO_LABEL}="modem"
ATTRS{idVendor}=="2020", ATTRS{idProduct}=="1005", ENV{ID_USB_INTERFACE_NUM}=="01", ENV{OFONO_LABEL}="aux"

ATTRS{idVendor}=="2020", ATTRS{idProduct}=="1008", ENV{ID_USB_INTERFACE_NUM}=="03", ENV{OFONO_LABEL}="modem"
ATTRS{idVendor}=="2020", ATTRS{idProduct}=="1008", ENV{ID_USB_INTERFACE_NUM}=="01", ENV{OFONO_LABEL}="aux"

# SpeedUp 9800
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9800", ENV{ID_USB_INTERFACE_NUM}=="01", ENV{OFONO_LABEL}="modem"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9800", ENV{ID_USB_INTERFACE_NUM}=="02", ENV{OFONO_LABEL}="aux"

# SpeedUp U3501
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{ID_USB_INTERFACE_NUM}=="03", ENV{OFONO_LABEL}="modem"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{ID_USB_INTERFACE_NUM}=="01", ENV{OFONO_LABEL}="aux"

LABEL="ofono_speedup_end"
`

type ofonoInterface struct{}

func (iface *ofonoInterface) Name() string {
	return "ofono"
}

func (iface *ofonoInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              ofonoSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: ofonoBaseDeclarationSlots,
	}
}

func (iface *ofonoInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(ofonoConnectedPlugAppArmor, old, new, -1))
	if release.OnClassic {
		// Let confined apps access unconfined ofono on classic
		spec.AddSnippet(ofonoConnectedPlugAppArmorClassic)
	}
	return nil

}

func (iface *ofonoInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(ofonoPermanentSlotAppArmor)
	return nil
}

func (iface *ofonoInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(ofonoPermanentSlotDBus)
	return nil
}

func (iface *ofonoInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(ofonoPermanentSlotUDev)
	/*
	   1.Linux modem drivers set up the modem device /dev/modem as a symbolic link
	     to the actual device to /dev/ttyS*
	   2./dev/socket/rild is just a socket, not device node created by rild daemon.
	     Similar case for chnlat*.
	   So we intetionally skipped modem, rild and chnlat.
	*/
	spec.TagDevice(`KERNEL=="tty[A-Z]*[0-9]*|cdc-wdm[0-9]*"`)
	spec.TagDevice(`KERNEL=="tun"`)
	spec.TagDevice(`KERNEL=="dsp"`)
	return nil
}

func (iface *ofonoInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	spec.AddSnippet(strings.Replace(ofonoConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *ofonoInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(ofonoPermanentSlotSecComp)
	return nil
}

func (iface *ofonoInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&ofonoInterface{})
}
