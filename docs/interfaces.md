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

The availability of an interface depends on whether snapd is running on a
classic (eg, traditional desktop or server) or on a native system. Interfaces
may also be implicit to the OS snap or implemented only via snaps providing the
slot.

## Making connections
Interfaces may either be auto-connected on install or manually connected after
install.

To list the available connectable interfaces and connections:

    $ snap interfaces

To make a connection:

    $ snap connect <snap>:<plug interface> <snap>:<slot interface>

To disconnect snaps:

    $ snap disconnect <snap>:<plug interface> <snap>:<slot interface>

Consider a snap ``foo`` that uses ``plugs: [ log-observe ]``. Since
``log-observe`` is not auto-connected, ``foo`` will not have access to the
interface upon install:

    $ sudo snap install foo
    $ snap interfaces
    Slot                 Plug
    :log-observe         -
    -                    foo:log-observe

You may manually connect using ``snappy connect``:

    $ sudo snap connect foo:log-observe core:log-observe
    $ snap interfaces
    Slot                 Plug
    :log-observe         foo:log-observe

and disconnect using ``snappy disconnect``:

    $ sudo snap disconnect foo:log-observe core:log-observe
    $ snap interfaces # shows they are disconnected
    Slot                 Plug
    :log-observe         -
    -                    foo:log-observe

On the other hand, ``bar`` could use ``plugs: [ network ]`` and since
``network`` is auto-connected, ``bar`` has access to the interface upon
install:

    $ sudo snap install bar
    $ snap interfaces
    Slot                 Plug
    :home                bar:home

You may disconnect an auto-connected interface:

    $ sudo snap disconnect bar:home core:home
    $ snap interfaces
    Slot                 Plug
    :home                -
    -                    bar:home

Whether the slot is implicit or not doesn't matter in terms of snap interfaces
except that if the slot is not implicit, a snap that implements the slot must
be installed for it to be connectable. Eg, the ``bluez`` interface is not
implicit so a snap author implementing the bluez service might use
``slots: [ bluez ]``. Then after install, the bluez interface shows up as
available:

    $ sudo snap install foo-blue
    $ snap interfaces
    Slot                 Plug
    foo-blue:bluez       -

Now install and connect works like before (eg, ``baz`` uses
``plugs: [ bluez ]``):

    $ sudo snap install baz
    $ snap interfaces
    Slot                 Plug
    foo-blue:bluez       -
    -                    baz:bluez
    $ sudo snap connect baz:bluez foo-blue:bluez
    $ snap interfaces
    Slot                 Plug
    foo-blue:bluez       baz:bluez

## Supported Interfaces - Basic

### camera

Can access the first video camera. Suitable for programs wanting to use the
webcams.

Usage: common
Auto-Connect: no
Availability: everywhere

### gsettings

Can access global gsettings of the user's session. This is restricted because
it gives privileged access to sensitive information stored in gsettings and
allows adjusting settings of other applications.

Usage: reserved
Auto-Connect: yes
Availability: classic

### home

Can access non-hidden files in user's `$HOME` to read/write/lock.
This is restricted because it gives file access to the user's
`$HOME`.

Usage: reserved
Auto-Connect: yes on classic, no on native
Availability: everywhere

### mpris

Can access media players implementing the Media Player Remote Interfacing
Specification (mpris) when the interface is specified as a plug.

Media players implementing mpris can be accessed by connected clients when
specified as a slot.

Usage: common
Auto-Connect: no
Availability: with providing snap

### network

Can access the network as a client.

Usage: common
Auto-Connect: yes
Availability: everywhere

### network-bind

Can access the network as a server.

Usage: common
Auto-Connect: yes
Availability: everywhere

### opengl

Can access the opengl hardware.

Usage: reserved
Auto-Connect: yes
Availability: classic

### optical-drive

Can access the first optical drive in read-only mode. Suitable for CD/DVD playback.

Usage: common
Auto-Connect: yes
Availability: classic

### pulseaudio

Can access the PulseAudio sound server. Allows for sound playback in games and
media application. It doesn't allow recording.

Usage: common
Auto-Connect: yes
Availability: classic

### unity7

Can access Unity7. Restricted because Unity 7 runs on X and requires access to
various DBus services and this environment does not prevent eavesdropping or
apps interfering with one another.

Usage: reserved
Auto-Connect: yes
Availability: classic

### x11

Can access the X server. Restricted because X does not prevent eavesdropping or
apps interfering with one another.

Usage: reserved
Auto-Connect: yes
Availability: classic

## Supported Interfaces - Advanced

### cups-control

Can access cups control socket. This is restricted because it provides
privileged access to configure printing.

Usage: reserved
Auto-Connect: no
Availability: classic

### firewall-control

Can configure firewall. This is restricted because it gives privileged access
to networking and should only be used with trusted apps.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### locale-control

Can manage locales directly separate from 'config core'.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### log-observe

Can read system logs and set kernel log rate-limiting.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### mount-observe

Can query system mount information. This is restricted because it gives
privileged read access to mount arguments and should only be used with trusted
apps.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### network-control

Can configure networking. This is restricted because it gives wide, privileged
access to networking and should only be used with trusted apps.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### network-observe

Can query network status information. This is restricted because it gives
privileged read-only access to networking information and should only be used
with trusted apps.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### serial-port

Can access serial ports. This is restricted because it provides privileged
access to configure serial port hardware.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### snapd-control

Can manage snaps via snapd.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### system-observe

Can query system status information. This is restricted because it gives
privileged read access to all processes on the system and should only be used
with trusted apps.

Usage: reserved
Auto-Connect: no
Availability: everywhere

### timeserver-control

Can manage timeservers directly separate from config core.

Usage: reserved
Auto-Connect: no
Availability: everywhere
