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

## Native vs classic interfaces
Native interfaces are designed for strong application isolation and user
control such that auto-connected interfaces are considered safe and users
choose what applications to trust and to what extent via manually connected
interfaces.

Interfaces available on classic are considered transitional since many of the
underlying technologies on classic systems were not designed with strong
application isolation in mind. Users should only install applications using
these interfaces from trusted sources.

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

Can access the first video camera. Suitable for programs wanting to use
webcams.

* Auto-Connect: no
* Availability: implicit (classic)

### gsettings

Can access global gsettings of the user's session which gives privileged access
to sensitive information stored in gsettings and allows adjusting settings of
other applications.

* Auto-Connect: yes
* Availability: implicit (classic)

### home

Can access non-hidden files in user's `$HOME` and gvfs mounted directories
owned by the user to read/write/lock.

* Auto-Connect: yes on classic, no on native
* Availability: implicit

### mpris

Providing snaps implementing the Media Player Remove Interfacing Specification
(mpris) may be accessed via their well-known DBus name.

Consuming snaps can access media players implementing mpris via the providing
snap's well-known DBus name.

* Auto-Connect: no
* Availability: with providing snap

### network

Can access the network as a client.

* Auto-Connect: yes
* Availability: implicit

### network-bind

Can access the network as a server.

* Auto-Connect: yes
* Availability: implicit

### opengl

Can access OpenGL hardware.

* Auto-Connect: yes
* Availability: implicit (classic)

### optical-drive

Can access the first optical drive in read-only mode. Suitable for CD/DVD
playback.

* Auto-Connect: yes
* Availability: implicit (classic)

### pulseaudio

Can access the PulseAudio sound server which allows for sound playback in games
and media application. Recording not supported but will be in a future release.

* Auto-Connect: yes
* Availability: implicit (classic)

### unity7

Can access Unity7. Unity 7 runs on X and requires access to various DBus
services. This interface grants privileged access to the user's session since
the Unity 7 environment does not prevent eavesdropping or apps interfering with
one another.

* Auto-Connect: yes
* Availability: implicit (classic)

### x11

Can access the X server which gives privileged access to the user's session
since X does not prevent eavesdropping or apps interfering with one another.

* Auto-Connect: yes
* Availability: implicit (classic)

## Supported Interfaces - Advanced

### bluez

Can access snaps providing the bluez interface which gives privileged access to
bluetooth.

* Auto-Connect: no
* Availability: with providing snap

### bool-file

Can access GPIO paths for LED brightness and GPIO values.

* Auto-Connect: no
* Availability: implicit
Attributes:
- path: path to GPIO bool file

### content

Can access content from the providing snap from within the consuming snap's
filesystem area.

* Auto-Connect: yes for snaps from same publisher, no otherwise
* Availability: with providing snap
Attributes:
- read: path from providing snap to expose read-only to the consuming snap
- write: path from providing snap to expose read-write to the consuming snap

### cups-control

Can access cups control socket which gives privileged access to configure
printing.

* Auto-Connect: no
* Availability: implicit (classic)

### firewall-control

Can configure network firewalling giving privileged access to networking.

* Auto-Connect: no
* Availability: implicit

### locale-control

Can manage locales directly separate from ``config core``.

* Auto-Connect: no
* Availability: implicit

### log-observe

Can read system logs and set kernel log rate-limiting.

* Auto-Connect: no
* Availability: implicit

### modem-manager

Can access snaps providing the modem-manager interface which gives privileged
access to configure, observe and use modems.

* Auto-Connect: no
* Availability: implicit (classic), with providing snap (native)

### mount-observe

Can query system mount information. This is restricted because it gives
privileged read access to mount arguments and should only be used with trusted
apps.

* Auto-Connect: no
* Availability: implicit

### network-control

Can configure networking which gives wide, privileged access to networking.

* Auto-Connect: no
* Availability: implicit

### network-manager

Can access snaps providing the network-manager interface which gives privileged
access to configure and observe networking.

* Auto-Connect: no
* Availability: implicit (classic), with providing snap (native)

### network-observe

Can query network status information which gives privileged read-only access to
networking information.

* Auto-Connect: no
* Availability: implicit

### ppp

Can access Point-to-Point protocol daemon which gives privileged access to
configure and observe PPP networking.

* Auto-Connect: no
* Availability: implicit

### serial-port

Can access serial ports. This is restricted because it provides privileged
access to configure serial port hardware.

* Auto-Connect: no
* Availability: implicit

### snapd-control

Can manage snaps via snapd.

* Auto-Connect: no
* Availability: implicit

### system-observe

Can query system status information which gives privileged read access to all
processes on the system.

* Auto-Connect: no
* Availability: implicit

### timeserver-control

Can manage timeservers directly separate from ``config core``.

* Auto-Connect: no
* Availability: implicit
