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

const networkManagerSummary = `allows operating as the NetworkManager service`

const networkManagerBaseDeclarationSlots = `
  network-manager:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const networkManagerPermanentSlotAppArmor = `
# Description: Allow operating as the NetworkManager service. This gives
# privileged access to the system.

capability net_admin,
capability net_bind_service,
capability net_raw,

network netlink,
network bridge,
network inet,
network inet6,
network packet,

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

# used by sysctl, et al
@{PROC}/sys/ r,
@{PROC}/sys/net/ r,
@{PROC}/sys/net/core/ r,
@{PROC}/sys/net/core/** rw,
@{PROC}/sys/net/ipv{4,6}/ r,
@{PROC}/sys/net/ipv{4,6}/** rw,
@{PROC}/sys/net/netfilter/ r,
@{PROC}/sys/net/netfilter/** rw,
@{PROC}/sys/net/nf_conntrack_max rw,

# Needed for systemd's dhcp implementation
@{PROC}/sys/kernel/random/boot_id r,

/sys/devices/**/**/net/**/phys_port_id r,
/sys/devices/**/**/net/**/dev_id r,
/sys/devices/virtual/net/**/phys_port_id r,
/sys/devices/virtual/net/**/dev_id r,
/sys/devices/**/net/**/ifindex r,

# access to bridge sysfs interfaces for bridge settings
/sys/devices/virtual/net/*/bridge/* rw,

/dev/rfkill rw,

/run/udev/data/* r,

/etc/gnutls/config r,

# Allow read and write access for all netplan configuration files
# as NetworkManager will start using them to store the network
# configuration instead of using its own internal keyfile based
# format.
/etc/netplan/{,**} rw,

# Allow access to configuration files generated on the fly
# from netplan and let NetworkManager store its configuration
# in the same place.
/run/NetworkManager/{,**} rw,

# Allow access to the system dbus
/run/dbus/system_bus_socket rw,

# Needed by the ifupdown plugin to check which interfaces can
# be managed an which not.
/etc/network/interfaces r,
# Needed for systemd's dhcp implementation
/etc/machine-id r,

# Needed to use resolvconf from core
/{,usr/}sbin/resolvconf ixr,
/run/resolvconf/{,**} rk,
/run/resolvconf/** w,
/etc/resolvconf/{,**} r,
/{,usr/}lib/resolvconf/* ix,
# NM peeks into ifupdown configuration
/run/network/ifstate* r,
# Required by resolvconf
/{,usr/}bin/run-parts ixr,
/etc/resolvconf/update.d/* ix,

#include <abstractions/nameservice>
/run/systemd/resolve/stub-resolv.conf r,

# DBus accesses
#include <abstractions/dbus-strict>

# systemd-resolved (not yet included in nameservice abstraction)
#
# Allow access to the safe members of the systemd-resolved D-Bus API:
#
#   https://www.freedesktop.org/software/systemd/man/org.freedesktop.resolve1.html
#
# This API may be used directly over the D-Bus system bus or it may be used
# indirectly via the nss-resolve plugin:
#
#   https://www.freedesktop.org/software/systemd/man/nss-resolve.html
#
# In the case of NM, the destination is not the well-known DBus name,
# instead it tracks the name owner and sends the message to the
# the owner's connection name, so we cannot have the name= restriction
# in peer=...
dbus send
     bus=system
     path="/org/freedesktop/resolve1"
     interface="org.freedesktop.resolve1.Manager"
     member="Resolve{Address,Hostname,Record,Service}"
     peer=(label=unconfined),

dbus (send)
     bus=system
     path="/org/freedesktop/resolve1"
     interface="org.freedesktop.resolve1.Manager"
     member="SetLink{DefaultRoute,DNSOverTLS,DNS,DNSEx,DNSSEC,DNSSECNegativeTrustAnchors,MulticastDNS,Domains,LLMNR}"
     peer=(label=unconfined),

dbus (send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member={Request,Release}Name
   peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (receive, send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member=GetConnectionUnixProcessID
   peer=(label=unconfined),

dbus (receive, send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member=GetConnectionUnixUser
   peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.freedesktop.NetworkManager",
# Allow binding OpenVPN names
dbus (bind)
    bus=system
    name="org.freedesktop.NetworkManager.openvpn.*",

# Allow traffic to/from our path and interface with any method for unconfined
# clients to talk to our service.
dbus (receive, send)
    bus=system
    path=/org/freedesktop/NetworkManager{,/**}
    interface=org.freedesktop.NetworkManager*
    peer=(label=unconfined),

# Allow traffic to/from org.freedesktop.DBus for NetworkManager service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/NetworkManager{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

# Allow ObjectManager methods from and signals to unconfined clients.
dbus (receive, send)
    bus=system
    path=/org/freedesktop
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),

# Allow access to hostname system service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}",

dbus(receive, send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.hostname1
    member={Set,SetStatic}Hostname
    peer=(label=unconfined),
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.hostname1
    member={Set,SetStatic}Hostname,

# Sleep monitor inside NetworkManager needs this
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/login1
    member=Inhibit
    interface=org.freedesktop.login1.Manager,
dbus (receive)
    bus=system
    path=/org/freedesktop/login1
    member=PrepareForSleep
    interface=org.freedesktop.login1.Manager
    peer=(label=unconfined),
dbus (receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=Session{New,Removed}
    peer=(label=unconfined),

# Allow access to wpa-supplicant for managing WiFi networks
dbus (receive, send)
    bus=system
    path=/fi/w1/wpa_supplicant1{,/**}
    interface=fi.w1.wpa_supplicant1*
    peer=(label=unconfined),
dbus (receive, send)
    bus=system
    path=/fi/w1/wpa_supplicant1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

# Allow network manager to manage netplan configuration
dbus (send)
    bus=system
    interface=io.netplan.Netplan
    path=/io/netplan/Netplan
    member=Apply
    peer=(label=unconfined),
`

const networkManagerConnectedSlotAppArmor = `
# Allow connected clients to interact with the service

# Allow traffic to/from our DBus path
dbus (receive, send)
    bus=system
    path=/org/freedesktop/NetworkManager{,/**}
    peer=(label=###PLUG_SECURITY_TAGS###),

# Later versions of NetworkManager implement org.freedesktop.DBus.ObjectManager
# for clients to easily obtain all (and be alerted to added/removed) objects
# from the service.
dbus (receive, send)
    bus=system
    path=/org/freedesktop
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=###PLUG_SECURITY_TAGS###),

# Explicitly deny ptrace to silence noisy denials. These denials happen when NM
# tries to access /proc/<peer_pid>/stat.  What apparmor prevents is showing
# internal process addresses that live in that file, but that has no adverse
# effects for NetworkManager, which just wants to find out the start time of the
# process.
deny ptrace (trace,read) peer=###PLUG_SECURITY_TAGS###,
`

const networkManagerConnectedPlugAppArmor = `
# Description: Allow using NetworkManager service. This gives privileged access
# to the NetworkManager service.

#include <abstractions/dbus-strict>

# Allow all access to NetworkManager service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/NetworkManager{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),

# NM implements org.freedesktop.DBus.ObjectManager too
dbus (receive, send)
    bus=system
    path=/org/freedesktop
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=###SLOT_SECURITY_TAGS###),

# nmcli uses this in newer versions
dbus (send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member=GetConnectionUnixUser
   peer=(label=unconfined),
`

const networkManagerConnectedPlugIntrospectionSnippet = `
# Allow us to introspect the network-manager providing snap
dbus (send)
    bus=system
    interface="org.freedesktop.DBus.Introspectable"
    member="Introspect"
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const networkManagerConnectedSlotIntrospectionSnippet = `
# Allow plugs to introspect us
dbus (receive)
    bus=system
    interface="org.freedesktop.DBus.Introspectable"
    member="Introspect"
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const networkManagerConnectedPlugSecComp = `
# Description: This is needed to talk to the network-manager service
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const networkManagerPermanentSlotSecComp = `
# Description: Allow operating as the NetworkManager service. This gives
# privileged access to the system.
accept
accept4
bind
listen
sethostname
# netlink
socket AF_NETLINK - -
`

const networkManagerPermanentSlotDBus = `
<!-- DBus policy for NetworkManager (upstream version 1.2.2) -->
<policy user="root">
    <allow own="org.freedesktop.NetworkManager"/>
    <allow send_destination="org.freedesktop.NetworkManager"/>

    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.PPP"/>

    <allow send_interface="org.freedesktop.NetworkManager.SecretAgent"/>

    <!-- These are there because some broken policies do
         <deny send_interface="..." /> (see dbus-daemon(8) for details).
         This seems to override that for the known VPN plugins. -->
    <allow send_destination="org.freedesktop.NetworkManager.openconnect"/>
    <allow send_destination="org.freedesktop.NetworkManager.openswan"/>
    <allow send_destination="org.freedesktop.NetworkManager.openvpn"/>
    <allow send_destination="org.freedesktop.NetworkManager.pptp"/>
    <allow send_destination="org.freedesktop.NetworkManager.vpnc"/>
    <allow send_destination="org.freedesktop.NetworkManager.ssh"/>
    <allow send_destination="org.freedesktop.NetworkManager.iodine"/>
    <allow send_destination="org.freedesktop.NetworkManager.l2tp"/>
    <allow send_destination="org.freedesktop.NetworkManager.libreswan"/>
    <allow send_destination="org.freedesktop.NetworkManager.fortisslvpn"/>
    <allow send_destination="org.freedesktop.NetworkManager.strongswan"/>
    <allow send_interface="org.freedesktop.NetworkManager.VPN.Plugin"/>

    <!-- Allow the custom name for the dnsmasq instance spawned by NM
        from the dns dnsmasq plugin to own it's dbus name, and for
        messages to be sent to it.
    -->
    <allow own="org.freedesktop.NetworkManager.dnsmasq"/>
    <allow send_destination="org.freedesktop.NetworkManager.dnsmasq"/>

    <!-- VPN support -->
    <allow own_prefix="org.freedesktop.NetworkManager.openvpn"/>
    <allow send_destination="org.freedesktop.NetworkManager.openvpn"/>
</policy>

<policy context="default">
    <deny own="org.freedesktop.NetworkManager"/>

    <deny send_destination="org.freedesktop.NetworkManager"/>

    <!-- Basic D-Bus API stuff -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.DBus.Introspectable"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.DBus.Properties"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.DBus.ObjectManager"/>

    <!-- Devices (read-only properties, no methods) -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Adsl"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Bond"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Bridge"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Bluetooth"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Wired"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Generic"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Gre"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Infiniband"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Macvlan"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Modem"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.OlpcMesh"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Team"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Tun"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Veth"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Vlan"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.WiMax.Nsp"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.AccessPoint"/>

    <!-- Devices (read-only, no security required) -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.WiMax"/>

    <!-- Devices (read/write, secured with PolicyKit) -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device.Wireless"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Device"/>

    <!-- Core stuff (read-only properties, no methods) -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Connection.Active"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.DHCP4Config"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.DHCP6Config"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.IP4Config"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.IP6Config"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.VPN.Connection"/>

    <!-- Core stuff (read/write, secured with PolicyKit) -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Settings"/>
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.Settings.Connection"/>

    <!-- Agents; secured with PolicyKit.  Any process can talk to
         the AgentManager API, but only NetworkManager can talk
         to the agents themselves. -->
    <allow send_destination="org.freedesktop.NetworkManager"
           send_interface="org.freedesktop.NetworkManager.AgentManager"/>

    <!-- Root-only functions -->
    <deny send_destination="org.freedesktop.NetworkManager"
          send_interface="org.freedesktop.NetworkManager"
          send_member="SetLogging"/>
    <deny send_destination="org.freedesktop.NetworkManager"
          send_interface="org.freedesktop.NetworkManager"
          send_member="Sleep"/>
    <deny send_destination="org.freedesktop.NetworkManager"
          send_interface="org.freedesktop.NetworkManager.Settings"
          send_member="LoadConnections"/>
    <deny send_destination="org.freedesktop.NetworkManager"
          send_interface="org.freedesktop.NetworkManager.Settings"
          send_member="ReloadConnections"/>

    <deny own="org.freedesktop.NetworkManager.dnsmasq"/>
    <deny send_destination="org.freedesktop.NetworkManager.dnsmasq"/>

    <!-- VPN support -->
    <deny own_prefix="org.freedesktop.NetworkManager.openvpn"/>
    <deny send_destination="org.freedesktop.NetworkManager.openvpn"/>
</policy>

<limit name="max_replies_per_connection">1024</limit>
<limit name="max_match_rules_per_connection">2048</limit>
`

type networkManagerInterface struct{}

func (iface *networkManagerInterface) Name() string {
	return "network-manager"
}

func (iface *networkManagerInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              networkManagerSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: networkManagerBaseDeclarationSlots,
	}
}

func (iface *networkManagerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if release.OnClassic {
		// If we're running on classic NetworkManager will be part
		// of the OS snap and will run unconfined.
		new = "unconfined"
	} else {
		new = spec.SnapAppSet().SlotLabelExpression(slot)
	}
	snippet := strings.Replace(networkManagerConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	if !release.OnClassic {
		// See https://bugs.launchpad.net/snapd/+bug/1849291 for details.
		snippet := strings.Replace(networkManagerConnectedPlugIntrospectionSnippet, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *networkManagerInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	snippet := strings.Replace(networkManagerConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	if !release.OnClassic {
		// See https://bugs.launchpad.net/snapd/+bug/1849291 for details.
		snippet := strings.Replace(networkManagerConnectedSlotIntrospectionSnippet, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *networkManagerInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(networkManagerPermanentSlotAppArmor)
	return nil
}

func (iface *networkManagerInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(networkManagerPermanentSlotDBus)
	return nil
}

func (iface *networkManagerInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(networkManagerPermanentSlotSecComp)
	return nil
}

func (iface *networkManagerInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TagDevice(`KERNEL=="rfkill"`)
	return nil
}

func (iface *networkManagerInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(networkManagerConnectedPlugSecComp)
	return nil
}

func (iface *networkManagerInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&networkManagerInterface{})
}
