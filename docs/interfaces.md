# Interfaces

Interfaces allow snaps to communicate or share resources according to the
protocol established by the interface.

Each connection has two ends, a "plug" (consumer) and a "slot" (provider).  A
plug and a slot can be connected if they use the same interface name.  The
connection grants necessary permissions for snaps to operate according to the
protocol.

Slots may support multiple connections to plugs.  For example the OS snap
exposes the ``network`` slot and all applications that can talk over the
network connect their plugs there.

## Supported Interfaces - Basic

### network

Can access the network as a client.

Usage: common
Auto-Connect: yes

### network-bind

Can access the network as a server.

Usage: common
Auto-Connect: yes

### unity7

Can access Unity7. Restricted because Unity 7 runs on X and requires access to
various DBus services and this environment does not prevent eavesdropping or
apps interfering with one another.

Usage: reserved
Auto-Connect: yes

### x11

Can access the X server. Restricted because X does not prevent eavesdropping or
apps interfering with one another.

Usage: reserved
Auto-Connect: yes

### home

Can access non-hidden files in user's $HOME. This is restricted
because it gives file access to all of the user's $HOME.

Usage: reserved

## Supported Interfaces - Advanced

### firewall-control

Can configure firewall. This is restricted because it gives privileged access
to networking and should only be used with trusted apps.

Usage: reserved

### locale-control

Can manage locales directly separate from 'config ubuntu-core'.

Usage: reserved

### log-observe

Can read system logs.

Usage: reserved

### mount-observe

Can query system mount information. This is restricted because it gives
privileged read access to mount arguments and should only be used with trusted
apps.

Usage: reserved

### network-control

Can configure networking. This is restricted because it gives wide, privileged
access to networking and should only be used with trusted apps.

Usage: reserved

### network-observe

Can query network status information. This is restricted because it gives
privileged read-only access to networking information and should only be used
with trusted apps.

Usage: reserved

### snapd-control

Can manage snaps via snapd.

Usage: reserved

### system-observe

Can query system status information. This is restricted because it gives
privileged read access to all processes on the system and should only be used
with trusted apps.

Usage: reserved

### timeserver-control

Can manage timeservers directly separate from config ubuntu-core.

Usage: reserved

