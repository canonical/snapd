# Interfaces

Interfaces allow snaps to communicate or share resources according to the
protocol defined by the interface.

Each connection has two ends, a “plug” (consumer) and a “slot” (provider).  A
plug and a slot can be connected if they use the same interface name.  The
connection grants necessary permissions for snaps to operate according to the
protocol.

Slots may support multiple connections to plugs.  For example the OS snap
exposes the ``network`` slot and all applications that can talk over the
network connect their plugs there.

Plugs may support multiple connections to slots. There are no examples of this
functionality at this time.

## Supported Interfaces

### firewall-control

Description: Can configure firewall. This is restricted because it gives
privileged access to networking and should only be used with trusted apps.
Usage: reserved

### home

Description: Can access non-hidden files in user's $HOME. This is restricted
because it gives file access to all of the user's $HOME.
Usage: reserved

### locale-control

Description: Can manage locales directly separate from 'config ubuntu-core'.
Usage: reserved

### log-observe

Description: Can read system logs.
Usage: reserved

### mount-observe

Description: Can query system mount information. This is restricted because it
gives privileged read access to mount arguments and should only be used with
trusted apps.
Usage: reserved

### network

Description: Can access the network as a client.
Usage: common

### network-bind

Description: Can access the network as a server.
Usage: common

### network-control

Description: Can configure networking. This is restricted because it gives
wide, privileged access to networking and should only be used with trusted
apps.
Usage: reserved

### network-observe

Description: Can query network status information. This is restricted because
it gives privileged read-only access to networking information and should only
be used with trusted apps.
Usage: reserved

### snap-control

Description: Can manage snaps via snapd.
Usage: reserved

### system-observe

Description: Can query system status information. This is restricted because it
gives privileged read access to all processes on the system and should only be
used with trusted apps.
Usage: reserved

### timeserver-control

Description: Can manage timeservers directly separate from config ubuntu-core.
Usage: reserved

### unity7

Description: Can access Unity7. Restricted because Unity 7 runs on X and
requires access to various DBus services and this enviroment does not prevent
eavesdropping or apps interfering with one another.
Usage: reserved

### x

Description: Can access the X server. Restricted because X does not prevent
eavesdropping or apps interfering with one another.
Usage: reserved
