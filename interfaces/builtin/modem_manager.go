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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const modemManagerSummary = `allows operating as the ModemManager service`

const modemManagerBaseDeclarationSlots = `
  modem-manager:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const modemManagerPermanentSlotAppArmor = `
# Description: Allow operating as the ModemManager service. This gives
# privileged access to the system.

# To check present devices
network netlink raw,
/run/udev/data/* r,
/sys/bus/usb/devices/ r,
# FIXME snapd should be querying udev and adding the /sys and /run/udev accesses
# that are assigned to the snap, but we are not there yet.
/sys/bus/usb/devices/** r,

# Access to modem ports
# FIXME snapd should be more dynamic to avoid conflicts between snaps trying to
# access same ports.
/dev/tty[^0-9]* rw,
/dev/cdc-* rw,

# For ioctl TIOCSSERIAL ASYNC_CLOSING_WAIT_NONE
capability sys_admin,

# For {mbim,qmi}-proxy
unix (bind, listen) type=stream addr="@{mbim,qmi}-proxy",
/sys/devices/**/usb**/descriptors r,
# See https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net-qmi
/sys/devices/**/net/*/qmi/* rw,

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
    name="org.freedesktop.ModemManager1",

# Allow traffic to/from our path and interface with any method for unconfined
# clients to talk to our modem-manager services.
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.ModemManager1*
    peer=(label=unconfined),
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

# Allow accessing logind services to properly shutdown devices on suspend
dbus (receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member={PrepareForSleep,SessionNew,SessionRemoved}
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=Inhibit
    peer=(label=unconfined),
`

const modemManagerConnectedSlotAppArmor = `
# Allow connected clients to interact with the service

# Allow traffic to/from our path and interface with any method
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.ModemManager1*
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow traffic to/from org.freedesktop.DBus for ModemManager service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const modemManagerConnectedPlugAppArmor = `
# Description: Allow using ModemManager service. This gives privileged access
# to the ModemManager service.

#include <abstractions/dbus-strict>

# Allow all access to ModemManager service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.ModemManager1*
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const modemManagerConnectedPlugAppArmorClassic = `
# Allow access to the unconfined ModemManager service on classic.
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.ModemManager1*
    peer=(label=unconfined),
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ModemManager1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),
`

const modemManagerPermanentSlotSecComp = `
# Description: Allow operating as the ModemManager service. This gives
# privileged access to the system.

# TODO: add ioctl argument filters when seccomp arg filtering is implemented
accept
accept4
bind
listen
# libgudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const modemManagerPermanentSlotDBus = `
<policy user="root">
    <allow own="org.freedesktop.ModemManager1"/>
    <allow send_destination="org.freedesktop.ModemManager1"/>
</policy>
`

const modemManagerConnectedPlugDBus = `
<policy context="default">
    <deny own="org.freedesktop.ModemManager1"/>
    <deny send_destination="org.freedesktop.ModemManager1"/>
</policy>
`

const modemManagerPermanentSlotUDev = `
# Concatenation of all ModemManager udev rules
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_cinterion_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_cinterion_port_types_end"
ENV{ID_VENDOR_ID}!="1e2d", GOTO="mm_cinterion_port_types_end"

SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

ATTRS{idVendor}=="1e2d", ATTRS{idProduct}=="0053", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_CINTERION_PORT_TYPE_GPS}="1"

LABEL="mm_cinterion_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_mbm_end"
SUBSYSTEMS=="usb", GOTO="mm_mbm_check"
GOTO="mm_mbm_end"

LABEL="mm_mbm_check"

# Ericsson F3507g
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1900", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1902", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F3607gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1904", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1905", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1906", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F3307
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="190a", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1909", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F3307 R2
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1914", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C3607w
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1049", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C3607w v2
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="190b", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F5521gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="190d", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1911", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson H5321gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1919", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson H5321w
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="191d", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F5321gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1917", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson F5321w
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="191b", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C5621gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="191f", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C5621w
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1921", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson H5321gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1926", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1927", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C3304w
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1928", ENV{ID_MM_ERICSSON_MBM}="1"

# Ericsson C5621 TFF
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="1936", ENV{ID_MM_ERICSSON_MBM}="1"

# Sony-Ericsson MD300
ATTRS{idVendor}=="0fce", ATTRS{idProduct}=="d0cf", ENV{ID_MM_ERICSSON_MBM}="1"

# Sony-Ericsson MD400
ATTRS{idVendor}=="0fce", ATTRS{idProduct}=="d0e1", ENV{ID_MM_ERICSSON_MBM}="1"

# Sony-Ericsson MD400G
ATTRS{idVendor}=="0fce", ATTRS{idProduct}=="d103", ENV{ID_MM_ERICSSON_MBM}="1"

# Dell 5560
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="818e", ENV{ID_MM_ERICSSON_MBM}="1"

# Dell 5550
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="818d", ENV{ID_MM_ERICSSON_MBM}="1"

# Dell 5530 HSDPA
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="8147", ENV{ID_MM_ERICSSON_MBM}="1"

# Dell F3607gw
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="8183", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="8184", ENV{ID_MM_ERICSSON_MBM}="1"

# Dell F3307
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="818b", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="413c", ATTRS{idProduct}=="818c", ENV{ID_MM_ERICSSON_MBM}="1"

# HP hs2330 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="271d", ENV{ID_MM_ERICSSON_MBM}="1"

# HP hs2320 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="261d", ENV{ID_MM_ERICSSON_MBM}="1"

# HP hs2340 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="3a1d", ENV{ID_MM_ERICSSON_MBM}="1"

# HP hs2350 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="3d1d", ENV{ID_MM_ERICSSON_MBM}="1"

# HP lc2000 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="301d", ENV{ID_MM_ERICSSON_MBM}="1"

# HP lc2010 Mobile Broadband Module
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="2f1d", ENV{ID_MM_ERICSSON_MBM}="1"

# Toshiba
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="130b", ENV{ID_MM_ERICSSON_MBM}="1"

# Toshiba F3607gw
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="130c", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1311", ENV{ID_MM_ERICSSON_MBM}="1"

# Toshiba F3307
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1315", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1316", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1317", ENV{ID_MM_ERICSSON_MBM}="1"

# Toshiba F5521gw
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1313", ENV{ID_MM_ERICSSON_MBM}="1"
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1314", ENV{ID_MM_ERICSSON_MBM}="1"

# Toshiba H5321gw
ATTRS{idVendor}=="0930", ATTRS{idProduct}=="1319", ENV{ID_MM_ERICSSON_MBM}="1"

# Lenovo N5321gw
ATTRS{idVendor}=="0bdb", ATTRS{idProduct}=="193e", ENV{ID_MM_ERICSSON_MBM}="1"

LABEL="mm_mbm_end"
# do not edit this file, it will be overwritten on update
ACTION!="add|change|move", GOTO="mm_huawei_port_types_end"

ENV{ID_VENDOR_ID}!="12d1", GOTO="mm_huawei_port_types_end"

# MU609 does not support getportmode (crashes modem with default firmware)
ATTRS{idProduct}=="1573", ENV{ID_MM_HUAWEI_DISABLE_GETPORTMODE}="1"

# Mark the modem and at port flags for ModemManager
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="01", ATTRS{bInterfaceProtocol}=="01", ENV{ID_MM_HUAWEI_MODEM_PORT}="1"
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="01", ATTRS{bInterfaceProtocol}=="02", ENV{ID_MM_HUAWEI_AT_PORT}="1"
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="02", ATTRS{bInterfaceProtocol}=="01", ENV{ID_MM_HUAWEI_MODEM_PORT}="1"
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="02", ATTRS{bInterfaceProtocol}=="02", ENV{ID_MM_HUAWEI_AT_PORT}="1"

# GPS NMEA port on MU609
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="01", ATTRS{bInterfaceProtocol}=="05", ENV{ID_MM_HUAWEI_GPS_PORT}="1"
# GPS NMEA port on MU909
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="ff", ATTRS{bInterfaceSubClass}=="01", ATTRS{bInterfaceProtocol}=="14", ENV{ID_MM_HUAWEI_GPS_PORT}="1"

# Only the standard ECM or NCM port can support dial-up with AT NDISDUP through AT port
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="02", ATTRS{bInterfaceSubClass}=="06",ATTRS{bInterfaceProtocol}=="00", ENV{ID_MM_HUAWEI_NDISDUP_SUPPORTED}="1"
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="02", ATTRS{bInterfaceSubClass}=="0d",ATTRS{bInterfaceProtocol}=="00", ENV{ID_MM_HUAWEI_NDISDUP_SUPPORTED}="1"

LABEL="mm_huawei_port_types_end"
# do not edit this file, it will be overwritten on update

# Longcheer makes modules that other companies rebrand, like:
#
# Alcatel One Touch X020
# Alcatel One Touch X030
# MobiData MBD-200HU
# ST Mobile Connect HSUPA USB Modem
#
# Most of these values were scraped from various Longcheer-based Windows
# driver .inf files.  cmmdm.inf lists the actual data (ie PPP) ports, while
# cmser.inf lists the aux ports that may be either AT-capable or not but
# cannot be used for PPP.


ACTION!="add|change|move", GOTO="mm_longcheer_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_longcheer_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="1c9e", GOTO="mm_longcheer_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="1bbb", GOTO="mm_tamobile_vendorcheck"
GOTO="mm_longcheer_port_types_end"

LABEL="mm_longcheer_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="3197", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="3197", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="3197", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6000", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6000", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6000", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6060", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6060", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6060", ENV{ID_MM_LONGCHEER_TAGGED}="1"

# Alcatel One Touch X020
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6061", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6061", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="6061", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7001", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7001", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7001", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7001", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7002", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7002", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7002", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7002", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7002", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7101", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7101", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7101", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7101", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7102", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7102", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7102", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7102", ENV{.MM_USBIFNUM}=="06", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="7102", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8000", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8000", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8000", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8000", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8001", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8001", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8001", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8001", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8002", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8002", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8002", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="8002", ENV{ID_MM_LONGCHEER_TAGGED}="1"

# ChinaBird PL68
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9000", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9000", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9000", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9001", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9001", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9001", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9001", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9002", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9002", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9002", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9002", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9003", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9003", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9003", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9003", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9003", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9004", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9004", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9004", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9005", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9005", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9005", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9010", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9010", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9010", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9010", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9012", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9012", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9012", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9012", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9020", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9020", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9020", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9020", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9022", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9022", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9022", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9022", ENV{ID_MM_LONGCHEER_TAGGED}="1"

# Zoom products
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9602", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9602", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9602", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9602", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9603", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9603", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9603", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9603", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9604", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9604", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9604", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9604", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9605", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9606", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9606", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9606", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9606", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9606", ENV{ID_MM_LONGCHEER_TAGGED}="1"

ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1c9e", ATTRS{idProduct}=="9607", ENV{ID_MM_LONGCHEER_TAGGED}="1"

GOTO="mm_longcheer_port_types_end"


LABEL="mm_tamobile_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# Alcatel One Touch X060s
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_LONGCHEER_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_LONGCHEER_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{ID_MM_LONGCHEER_TAGGED}="1"

GOTO="mm_longcheer_port_types_end"


LABEL="mm_longcheer_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_mtk_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="0e8d", GOTO="mm_mtk_port_types_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="2001", GOTO="mm_dlink_port_types_vendorcheck"
GOTO="mm_mtk_port_types_end"

# MediaTek devices ---------------------------

LABEL="mm_mtk_port_types_vendorcheck"
ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a1", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a1", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a1", ENV{ID_MM_MTK_TAGGED}="1"

ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a2", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a2", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a2", ENV{ID_MM_MTK_TAGGED}="1"

ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a4", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a4", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a4", ENV{ID_MM_MTK_TAGGED}="1"

ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a5", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a5", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a5", ENV{ID_MM_MTK_TAGGED}="1"

ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a7", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a7", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="00a7", ENV{ID_MM_MTK_TAGGED}="1"

GOTO="mm_mtk_port_types_end"

# D-Link devices ---------------------------

LABEL="mm_dlink_port_types_vendorcheck"
ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# D-Link DWM-156 A5 (and later?)
ATTRS{idVendor}=="2001", ATTRS{idProduct}=="7d00", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_MTK_MODEM_PORT}="1"
ATTRS{idVendor}=="2001", ATTRS{idProduct}=="7d00", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_MTK_AT_PORT}="1"
ATTRS{idVendor}=="2001", ATTRS{idProduct}=="7d00", ENV{ID_MM_MTK_TAGGED}="1"

GOTO="mm_mtk_port_types_end"

LABEL="mm_mtk_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_nokia_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_nokia_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="0421", GOTO="mm_nokia_port_types_vendorcheck"
GOTO="mm_nokia_port_types_end"

LABEL="mm_nokia_port_types_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# For Nokia Internet Sticks (CS-xx) the modem/PPP port appears to always be USB interface 1

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="060D", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0611", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="061A", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="061B", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="061F", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0619", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0620", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0623", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0624", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="0625", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="062A", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="062E", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

ATTRS{idVendor}=="0421", ATTRS{idProduct}=="062F", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_NOKIA_PORT_TYPE_MODEM}="1"

LABEL="mm_nokia_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_pcmcia_device_blacklist_end"
SUBSYSTEM!="pcmcia", GOTO="mm_pcmcia_device_blacklist_end"

# Gemplus Serial Port smartcard adapter
ATTRS{prod_id1}=="Gemplus", ATTRS{prod_id2}=="SerialPort", ATTRS{prod_id3}=="GemPC Card", ENV{ID_MM_DEVICE_IGNORE}="1"

LABEL="mm_pcmcia_device_blacklist_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_platform_device_whitelist_end"
SUBSYSTEM!="platform", GOTO="mm_platform_device_whitelist_end"

# Be careful here since many devices connected to platform drivers on PCs
# are legacy devices that won't like probing.  But often on embedded
# systems serial ports are provided by platform devices.

# Allow atmel_usart
DRIVERS=="atmel_usart", ENV{ID_MM_PLATFORM_DRIVER_PROBE}="1"

LABEL="mm_platform_device_whitelist_end"
# do not edit this file, it will be overwritten on update

# Simtech makes modules that other companies rebrand, like:
#
# A-LINK 3GU
# SCT UM300
#
# Most of these values were scraped from various SimTech-based Windows
# driver .inf files.  *mdm.inf lists the main command ports, while
# *ser.inf lists the aux ports that may be used for PPP.


ACTION!="add|change|move", GOTO="mm_simtech_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_simtech_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="1e0e", GOTO="mm_alink_vendorcheck"
GOTO="mm_simtech_port_types_end"

LABEL="mm_alink_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# A-LINK 3GU
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="cefe", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_SIMTECH_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="cefe", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_SIMTECH_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="cefe", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_SIMTECH_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="cefe", ENV{ID_MM_SIMTECH_TAGGED}="1"

# Prolink PH-300
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9100", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_SIMTECH_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9100", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_SIMTECH_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9100", ENV{ID_MM_SIMTECH_TAGGED}="1"

# SCT UM300
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9200", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_SIMTECH_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9200", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_SIMTECH_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1e0e", ATTRS{idProduct}=="9200", ENV{ID_MM_SIMTECH_TAGGED}="1"

GOTO="mm_simtech_port_types_end"

LABEL="mm_simtech_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_telit_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_telit_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="1bc7", GOTO="mm_telit_vendorcheck"
GOTO="mm_telit_port_types_end"

LABEL="mm_telit_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# UC864-E, UC864-E-AUTO, UC864-K, UC864-WD, UC864-WDU
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1003", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1003", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_TELIT_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1003", ENV{ID_MM_TELIT_TAGGED}="1"

# UC864-G
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1004", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1004", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_TELIT_PORT_TYPE_NMEA}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1004", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_TELIT_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1004", ENV{ID_MM_TELIT_TAGGED}="1"

# CC864-DUAL
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1005", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1005", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_TELIT_PORT_TYPE_NMEA}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1005", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_TELIT_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1005", ENV{ID_MM_TELIT_TAGGED}="1"

# CC864-SINGLE, CC864-KPS
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1006", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1006", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_TELIT_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1006", ENV{ID_MM_TELIT_TAGGED}="1"

# DE910-DUAL
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1010", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_TELIT_PORT_TYPE_NMEA}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1010", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_TELIT_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1010", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1010", ENV{ID_MM_TELIT_TAGGED}="1"

# CE910-DUAL
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1011", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_TELIT_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bc7", ATTRS{idProduct}=="1011", ENV{ID_MM_TELIT_TAGGED}="1"

# NOTE: Qualcomm Gobi-based devices like the LE920 should not be handled
# by this plugin, but by the Gobi plugin.

GOTO="mm_telit_port_types_end"
LABEL="mm_telit_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_usb_device_blacklist_end"
SUBSYSTEM!="usb", GOTO="mm_usb_device_blacklist_end"
ENV{DEVTYPE}!="usb_device",  GOTO="mm_usb_device_blacklist_end"

# Telegesis zigbee dongle
ATTRS{idVendor}=="10c4", ATTRS{idProduct}=="0003", ENV{ID_MM_DEVICE_IGNORE}="1"

# APC UPS devices
ATTRS{idVendor}=="051d", ENV{ID_MM_DEVICE_IGNORE}="1"

# Sweex 1000VA
ATTRS{idVendor}=="0925", ATTRS{idProduct}=="1234", ENV{ID_MM_DEVICE_IGNORE}="1"

# Agiler UPS
ATTRS{idVendor}=="05b8", ATTRS{idProduct}=="0000", ENV{ID_MM_DEVICE_IGNORE}="1"

# Krauler UP-M500VA
ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0000", ENV{ID_MM_DEVICE_IGNORE}="1"

# Ablerex 625L USB
ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="0000", ENV{ID_MM_DEVICE_IGNORE}="1"

# Belkin F6C1200-UNV
ATTRS{idVendor}=="0665", ATTRS{idProduct}=="5161", ENV{ID_MM_DEVICE_IGNORE}="1"

# Various Liebert and Phoenixtec Power devices
ATTRS{idVendor}=="06da", ENV{ID_MM_DEVICE_IGNORE}="1"

# Unitek Alpha 1200Sx
ATTRS{idVendor}=="0f03", ATTRS{idProduct}=="0001", ENV{ID_MM_DEVICE_IGNORE}="1"

# Various Tripplite devices
ATTRS{idVendor}=="09ae", ENV{ID_MM_DEVICE_IGNORE}="1"

# Various MGE Office Protection Systems devices
ATTRS{idVendor}=="0463", ATTRS{idProduct}=="0001", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="0463", ATTRS{idProduct}=="ffff", ENV{ID_MM_DEVICE_IGNORE}="1"

# CyberPower 900AVR/BC900D
ATTRS{idVendor}=="0764", ATTRS{idProduct}=="0005", ENV{ID_MM_DEVICE_IGNORE}="1"
# CyberPower CP1200AVR/BC1200D
ATTRS{idVendor}=="0764", ATTRS{idProduct}=="0501", ENV{ID_MM_DEVICE_IGNORE}="1"

# Various Belkin devices
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0980", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0900", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0910", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0912", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0551", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0751", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0375", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="1100", ENV{ID_MM_DEVICE_IGNORE}="1"

# HP R/T 2200 INTL (like SMART2200RMXL2U)
ATTRS{idVendor}=="03f0", ATTRS{idProduct}=="1f0a", ENV{ID_MM_DEVICE_IGNORE}="1"

# Powerware devices
ATTRS{idVendor}=="0592", ATTRS{idProduct}=="0002", ENV{ID_MM_DEVICE_IGNORE}="1"

# Palm Treo 700/900/etc
# Shouldn't be probed themselves, but you can install programs like
# "MobileStream USB Modem" which changes the USB PID of the device to something
# that isn't blacklisted.
ATTRS{idVendor}=="0830", ATTRS{idProduct}=="0061", ENV{ID_MM_DEVICE_IGNORE}="1"

# GlobalScaleTechnologies SheevaPlug
ATTRS{idVendor}=="9e88", ATTRS{idProduct}=="9e8f", ENV{ID_MM_DEVICE_IGNORE}="1"

# Atmel Corp at91sam SAMBA bootloader
ATTRS{idVendor}=="03eb", ATTRS{idProduct}=="6124", ENV{ID_MM_DEVICE_IGNORE}="1"

# Dangerous Prototypes Bus Pirate v4
ATTRS{idVendor}=="04d8", ATTRS{idProduct}=="fb00", ENV{ID_MM_DEVICE_IGNORE}="1"

# All devices from the Swiss Federal Institute of Technology
ATTRS{idVendor}=="0617", ENV{ID_MM_DEVICE_IGNORE}="1"

# West Mountain Radio devices
ATTRS{idVendor}=="10c4", ATTRS{idProduct}=="814a", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="10c4", ATTRS{idProduct}=="814b", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="2405", ATTRS{idProduct}=="0003", ENV{ID_MM_DEVICE_IGNORE}="1"

# Arduinos
ATTRS{idVendor}=="2341", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="1b4f", ATTRS{idProduct}=="9207", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="1b4f", ATTRS{idProduct}=="9208", ENV{ID_MM_DEVICE_IGNORE}="1"

# Adafruit Flora
ATTRS{idVendor}=="239a", ATTRS{idProduct}=="0004", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="239a", ATTRS{idProduct}=="8004", ENV{ID_MM_DEVICE_IGNORE}="1"

# All devices from Pololu Corporation
# except some possible future products.
ATTRS{idVendor}=="1ffb", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="1ffb", ATTRS{idProduct}=="00ad", ENV{ID_MM_DEVICE_IGNORE}="0"
ATTRS{idVendor}=="1ffb", ATTRS{idProduct}=="00ae", ENV{ID_MM_DEVICE_IGNORE}="0"

# Altair U-Boot device
ATTRS{idVendor}=="0216", ATTRS{idProduct}=="0051", ENV{ID_MM_DEVICE_IGNORE}="1"

# Bluegiga BLE112B
ATTRS{idVendor}=="2458", ATTRS{idProduct}=="0001", ENV{ID_MM_DEVICE_IGNORE}="1"

# Analog Devices BLIP camera
ATTRS{idVendor}=="064b", ATTRS{idProduct}=="7823", ENV{ID_MM_DEVICE_IGNORE}="1"

# MediaTek GPS chip (HOLUX M-1200E, GlobalTop Gms-d1, etc)
ATTRS{idVendor}=="0e8d", ATTRS{idProduct}=="3329", ENV{ID_MM_DEVICE_IGNORE}="1"

# PS-360 OEM (GPS sold with MS Street and Trips 2005)
ATTRS{idVendor}=="067b", ATTRS{idProduct}=="aaa0", ENV{ID_MM_DEVICE_IGNORE}="1"

# u-blox AG, u-blox 5 GPS chips
ATTRS{idVendor}=="1546", ATTRS{idProduct}=="01a5", ENV{ID_MM_DEVICE_IGNORE}="1"
ATTRS{idVendor}=="1546", ATTRS{idProduct}=="01a6", ENV{ID_MM_DEVICE_IGNORE}="1"

# Garmin GPS devices
DRIVERS=="garmin_gps", ENV{ID_MM_DEVICE_IGNORE}="1"

# Cypress M8-based GPS devices, UPSes, and serial converters
DRIVERS=="cypress_m8", ENV{ID_MM_DEVICE_IGNORE}="1"

# All devices in the Openmoko vendor ID
ATTRS{idVendor}=="1d50", ENV{ID_MM_DEVICE_IGNORE}="1"

# All devices from 3D Robotics
ATTRS{idVendor}=="26ac", ENV{ID_MM_DEVICE_IGNORE}="1"

# empiriKit science lab controller device
ATTRS{idVendor}=="0425", ATTRS{idProduct}=="0408", ENV{ID_MM_DEVICE_IGNORE}="1"

# Infineon Flashloader used by Intel XMM modem bootloader
ATTRS{idVendor}=="8087", ATTRS{idProduct}=="0716", ENV{ID_MM_DEVICE_IGNORE}="1"

LABEL="mm_usb_device_blacklist_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_usb_serial_adapters_greylist_end"
SUBSYSTEM!="usb", GOTO="mm_usb_serial_adapters_greylist_end"
ENV{DEVTYPE}!="usb_device",  GOTO="mm_usb_serial_adapters_greylist_end"

# Belkin F5U183 Serial Adapter
ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0103", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# FTDI-based serial adapters
#   FTDI does USB to serial converter ICs; and it's very likely that they'll
#   never do modems themselves, so it should be safe to add a rule only based
#   on the vendor Id.
ATTRS{idVendor}=="0403", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# ATEN Intl UC-232A (Prolific)
ATTRS{idVendor}=="0557", ATTRS{idProduct}=="2008", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# Prolific USB to Serial adapter
ATTRS{idVendor}=="067b", ATTRS{idProduct}=="2303", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# Magic Control Technology Corp adapters
ATTRS{idVendor}=="0711", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# Cygnal Integrated Products, Inc. CP210x
ATTRS{idVendor}=="10c4", ATTRS{idProduct}=="ea60", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"
ATTRS{idVendor}=="10c4", ATTRS{idProduct}=="ea71", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# QinHeng Electronics HL-340
ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# Atmel Corp. LUFA USB to Serial Adapter Project (Arduino)
ATTRS{idVendor}=="03eb", ATTRS{idProduct}=="204b", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

# Netchip Technology, Inc. Linux-USB Serial Gadget (CDC ACM mode)
ATTRS{idVendor}=="0525", ATTRS{idProduct}=="a4a7", ENV{ID_MM_DEVICE_MANUAL_SCAN_ONLY}="1"

LABEL="mm_usb_serial_adapters_greylist_end"
# do not edit this file, it will be overwritten on update

# Alcatel One Touch X220D
# Alcatel One Touch X200
#
# These values were scraped from the X220D's Windows .inf files.  jrdmdm.inf
# lists the actual command and data (ie PPP) ports, while jrdser.inf lists the
# aux ports that may be either AT-capable or not but cannot be used for PPP.


ACTION!="add|change|move", GOTO="mm_x22x_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_x22x_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="1bbb", GOTO="mm_x22x_generic_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="0b3c", GOTO="mm_x22x_olivetti_vendorcheck"
GOTO="mm_x22x_port_types_end"

# Generic JRD devices ---------------------------

LABEL="mm_x22x_generic_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# Alcatel X200
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_X22X_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0000", ENV{ID_MM_X22X_TAGGED}="1"

ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_X22X_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="0017", ENV{ID_MM_X22X_TAGGED}="1"

# Archos G9
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_X22X_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_X22X_PORT_TYPE_NMEA}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_X22X_PORT_TYPE_VOICE}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="1bbb", ATTRS{idProduct}=="00B7", ENV{ID_MM_X22X_TAGGED}="1"

GOTO="mm_x22x_port_types_end"

# Olivetti devices ---------------------------

LABEL="mm_x22x_olivetti_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

# Olicard 200
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_X22X_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{.MM_USBIFNUM}=="06", ENV{ID_MM_X22X_PORT_TYPE_AUX}="1"
ATTRS{idVendor}=="0b3c", ATTRS{idProduct}=="c005", ENV{ID_MM_X22X_TAGGED}="1"

GOTO="mm_x22x_port_types_end"

LABEL="mm_x22x_port_types_end"
# do not edit this file, it will be overwritten on update

ACTION!="add|change|move", GOTO="mm_zte_port_types_end"
SUBSYSTEM!="tty", GOTO="mm_zte_port_types_end"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="19d2", GOTO="mm_zte_port_types_vendorcheck"
GOTO="mm_zte_port_types_end"

LABEL="mm_zte_port_types_vendorcheck"
SUBSYSTEMS=="usb", ATTRS{bInterfaceNumber}=="?*", ENV{.MM_USBIFNUM}="$attr{bInterfaceNumber}"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0001", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0001", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0002", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0002", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0003", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0003", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0004", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0004", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0005", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0005", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0006", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0006", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0007", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0007", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0008", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0008", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0009", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0009", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="000A", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="000A", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0012", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0012", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0015", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0015", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0016", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0016", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0017", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0018", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0018", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0019", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0019", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0021", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0021", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0024", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0024", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0025", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0025", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0030", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0030", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0031", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0031", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0033", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0033", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0037", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0037", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0039", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0039", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0042", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0042", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0043", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0043", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0048", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0048", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0049", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0049", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0052", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0052", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0054", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0054", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0055", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0055", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0057", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0057", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0058", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0058", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0061", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0061", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0063", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0063", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0064", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0064", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0066", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0066", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0078", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0078", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0082", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0082", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0091", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0091", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0104", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0104", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0106", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0106", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0108", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0108", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0113", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0113", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0117", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0117", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0118", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0118", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0121", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0121", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0122", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0122", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0123", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0123", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0124", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0124", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0125", ENV{.MM_USBIFNUM}=="05", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0125", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0126", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0126", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0128", ENV{.MM_USBIFNUM}=="04", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="0128", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1007", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1007", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1008", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1008", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1010", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1010", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1254", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1254", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1515", ENV{.MM_USBIFNUM}=="00", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="1515", ENV{.MM_USBIFNUM}=="02", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="2002", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="2002", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="2003", ENV{.MM_USBIFNUM}=="03", ENV{ID_MM_ZTE_PORT_TYPE_MODEM}="1"
ATTRS{idVendor}=="19d2", ATTRS{idProduct}=="2003", ENV{.MM_USBIFNUM}=="01", ENV{ID_MM_ZTE_PORT_TYPE_AUX}="1"

# Icera-based devices that use DHCP, not AT%IPDPADDR
ATTRS{product}=="K3805-z", ENV{ID_MM_ZTE_ICERA_DHCP}="1"

LABEL="mm_zte_port_types_end"
# do not edit this file, it will be overwritten on update

# Tag any devices that MM might be interested in; if ModemManager is started
# up right after udev, when MM explicitly requests devices on startup it may
# get devices that haven't had all rules run yet.  Thus, we tag devices we're
# interested in and when handling devices during MM startup we ignore any
# that don't have this tag.  MM will still get the udev 'add' event for the
# device a short while later and then process it as normal.

ACTION!="add|change|move", GOTO="mm_candidate_end"

SUBSYSTEM=="tty", ENV{ID_MM_CANDIDATE}="1"
SUBSYSTEM=="net", ENV{ID_MM_CANDIDATE}="1"
KERNEL=="cdc-wdm*", SUBSYSTEM=="usb", ENV{ID_MM_CANDIDATE}="1"
KERNEL=="cdc-wdm*", SUBSYSTEM=="usbmisc", ENV{ID_MM_CANDIDATE}="1"

LABEL="mm_candidate_end"
`

type modemManagerInterface struct{}

func (iface *modemManagerInterface) Name() string {
	return "modem-manager"
}

func (iface *modemManagerInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              modemManagerSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: modemManagerBaseDeclarationSlots,
	}
}

func (iface *modemManagerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(modemManagerConnectedPlugAppArmor, old, new, -1))
	if release.OnClassic {
		// Let confined apps access unconfined ofono on classic
		spec.AddSnippet(modemManagerConnectedPlugAppArmorClassic)
	}
	return nil
}

func (iface *modemManagerInterface) DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(modemManagerConnectedPlugDBus)
	return nil
}

func (iface *modemManagerInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(modemManagerPermanentSlotAppArmor)
	return nil
}

func (iface *modemManagerInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(modemManagerPermanentSlotDBus)
	return nil
}

func (iface *modemManagerInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(modemManagerPermanentSlotUDev)
	spec.TagDevice(`KERNEL=="tty[A-Z]*[0-9]*|cdc-wdm[0-9]*"`)
	return nil
}

func (iface *modemManagerInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(modemManagerConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *modemManagerInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(modemManagerPermanentSlotSecComp)
	return nil
}

func (iface *modemManagerInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&modemManagerInterface{})
}
