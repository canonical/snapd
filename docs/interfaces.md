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

### pulseaudio

Can access the PulseAudio sound server. Allows for sound playback in games and
media application. It doesn't allow recording.

Usage: common
Auto-Connect: yes

### opengl

Can access the opengl hardware.

Usage: reserved
Auto-Connect: yes

### home

Can access non-hidden files in user's `$HOME` to read/write/lock.
This is restricted because it gives file access to the user's
`$HOME`. This interface is auto-connected on classic systems and
manually connected on non-classic.

Usage: reserved
Auto-Connect: yes

### gsettings

Can access global gsettings of the user's session. This is restricted because
it gives privileged access to sensitive information stored in gsettings and
allows adjusting settings of other applications.

Usage: reserved
Auto-Connect: yes

### optical-drive

Can access the first optical drive in read-only mode. Suitable for CD/DVD playback.

Usage: common
Auto-Connect: yes

### mpris

Can access media players implementing the Media Player Remote Interfacing
Specification (mpris) when the interface is specified as a plug.

Media players implementing mpris can be accessed by connected clients when
specified as a slot.

Usage: common
Auto-Connect: no

### camera

Can access the first video camera. Suitable for programs wanting to use the
webcams.

Usage: common
Auto-Connect: no

## Supported Interfaces - Advanced

### cups-control

Can access cups control socket. This is restricted because it provides
privileged access to configure printing.

Usage: reserved
Auto-Connect: no

### firewall-control

Can configure firewall. This is restricted because it gives privileged access
to networking and should only be used with trusted apps.

Usage: reserved
Auto-Connect: no

### locale-control

Can manage locales directly separate from 'config ubuntu-core'.

Usage: reserved
Auto-Connect: no

### log-observe

Can read system logs and set kernel log rate-limiting.

Usage: reserved
Auto-Connect: no

### mount-observe

Can query system mount information. This is restricted because it gives
privileged read access to mount arguments and should only be used with trusted
apps.

Usage: reserved
Auto-Connect: no

### network-control

Can configure networking. This is restricted because it gives wide, privileged
access to networking and should only be used with trusted apps.

Usage: reserved
Auto-Connect: no

### network-observe

Can query network status information. This is restricted because it gives
privileged read-only access to networking information and should only be used
with trusted apps.

Usage: reserved
Auto-Connect: no

### serial-port

Can access serial ports. This is restricted because it provides privileged
access to configure serial port hardware.

Usage: reserved
Auto-Connect: no

### snapd-control

Can manage snaps via snapd.

Usage: reserved
Auto-Connect: no

### system-observe

Can query system status information. This is restricted because it gives
privileged read access to all processes on the system and should only be used
with trusted apps.

Usage: reserved
Auto-Connect: no

### timeserver-control

Can manage timeservers directly separate from config ubuntu-core.

Usage: reserved
Auto-Connect: no

### hardware-observe

Can query hardware information from the system.

Usage: reserved
Auto-Connect: no