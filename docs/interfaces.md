# Interfaces

Interfaces allow snaps to communicate or share resources according to the
protocol established by the interface.

Each connection has two ends, a "plug" (consumer) and a "slot" (provider).  A
plug and a slot can be connected if they use the same interface name.  The
connection grants necessary permissions for snaps to operate according to the
protocol.

Slots may support multiple connections to plugs.  For example the core snap
exposes the ``network`` slot and all applications that can talk over the
network connect their plugs there.

The availability of an interface depends on a number of factors and may be
may be provided by the core snap or via snaps providing the slot.  The
available interfaces on a given system can be seen with ``snap interfaces``.

## Transitional interfaces
Most interfaces are designed for strong application isolation and user control
such that auto-connected interfaces are considered safe and users choose what
applications to trust and to what extent via manually connected interfaces.

Some interfaces are considered transitional to support traditional Linux
desktop environments and these transitional interfaces typically are
auto-connected. Since many of the underlying technologies in these environments
were not designed with strong application isolation in mind, users should only
install applications using these interfaces from trusted sources.  Transitional
interfaces will be deprecated as replacement or modified technologies that
enforce strong application isolation are available.

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

You may manually connect using ``snap connect``:

    $ sudo snap connect foo:log-observe core:log-observe
    $ snap interfaces
    Slot                 Plug
    :log-observe         foo:log-observe

and disconnect using ``snap disconnect``:

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
    :network             bar:network

You may disconnect an auto-connected interface:

    $ sudo snap disconnect bar:network core:network
    $ snap interfaces
    Slot                 Plug
    :network             -
    -                    bar:network

Whether the slot is provided by the core snap or not doesn't matter in terms of
snap interfaces except that if the slot is provided by a snap, a snap that
implements the slot must be installed for it to be connectable. Eg, the
``bluez`` interface is not provided by the core snap so a snap author
implementing the bluez service might use ``slots: [ bluez ]``. Then after
install, the bluez interface shows up as available:

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

### gsettings

Can access global gsettings of the user's session which gives privileged access
to sensitive information stored in gsettings and allows adjusting settings of
other applications.

* Auto-Connect: yes
* Transitional: yes

### home

Can access non-hidden files in user's `$HOME` and gvfs mounted directories
owned by the user to read/write/lock.

* Auto-Connect: yes on classic (traditional distributions), no otherwise
* Transitional: yes

### mpris

Providing snaps implementing the Media Player Remove Interfacing Specification
(mpris) may be accessed via their well-known DBus name.

Consuming snaps can access media players implementing mpris via the providing
snap's well-known DBus name.

* Auto-Connect: no
* Attributes:
    * name (slot): optional, media player name to use for DBus well-known name
      (ie, `org.mpris.MediaPlayer2.$name`). If omitted, use the snap's name.

### network

Can access the network as a client.

* Auto-Connect: yes

### network-bind

Can access the network as a server.

* Auto-Connect: yes

### opengl

Can access OpenGL hardware.

* Auto-Connect: yes

### optical-drive

Can access the first optical drive in read-only mode. Suitable for CD/DVD
playback.

* Auto-Connect: yes

### pulseaudio

Can access the PulseAudio sound server which allows for sound playback in games
and media application. Recording not supported but will be in a future release.

* Auto-Connect: yes

### unity7

Can access Unity7. Unity 7 runs on X and requires access to various DBus
services. This interface grants privileged access to the user's session since
the Unity 7 environment does not prevent eavesdropping or apps interfering with
one another.

* Auto-Connect: yes
* Transitional: yes

### x11

Can access the X server which gives privileged access to the user's session
since X does not prevent eavesdropping or apps interfering with one another.

* Auto-Connect: yes
* Transitional: yes

## Supported Interfaces - Advanced

### browser-support

Can access files and IPC needed by modern browsers. This interface is
intended to be used when using an embedded Chromium Content API or using the
sandboxes in major browsers from vendors like Google and Mozilla. The
``allow-sandbox`` attribute may be used to give the necessary access to use
the browser's sandbox functionality.

* Auto-Connect: yes
* Attributes:
    * allow-sandbox: true|false (defaults to ``false``)

### bluetooth-control

Allow to manage the kernel side Bluetooth stack.

* Auto-Connect: no

### bluez

Can access snaps providing the bluez interface which gives privileged access to
bluetooth.

* Auto-Connect: no

### content

Can access content from the providing snap from within the consuming snap's
filesystem area.

* Auto-Connect: yes for snaps from same publisher, no otherwise
* Attributes:
    * read (slot): read-only paths from providing snap to expose to the consuming snap
    * write (slot): read-write paths from providing snap to expose to the consuming snap
    * target (plug): path in consuming snap to find providing snap's files

### cups-control

Can access cups control socket which gives privileged access to configure
printing.

* Auto-Connect: no

### firewall-control

Can configure network firewalling giving privileged access to networking.

* Auto-Connect: no

### fuse-support

Can mount fuse filesystems (as root only).

* Auto-Connect: no

### hardware-observe

Can query hardware information from the system.

* Auto-Connect: no

### kernel-module-control

Can insert kernel modules. This interface gives privileged access to the device.

* Auto-Connect: no

### locale-control

Can manage locales directly separate from ``config core``.

* Auto-Connect: no

### location-control

Can access snaps providing the location-control interface which gives
privileged access to configure, observe and use location services.

* Auto-Connect: no

### location-observe

Can access snaps providing the location-observe interface which gives
privileged access to query location services.

* Auto-Connect: no

### log-observe

Can read system logs and set kernel log rate-limiting.

* Auto-Connect: no

### lxd-support
Can access all resources and syscalls on the device for LXD to mediate access
for its containers. This interface currently may only be used by the lxd snap
from Canonical.

* Auto-Connect: yes
* Transitional: yes

### modem-manager

Can access snaps providing the modem-manager interface which gives privileged
access to configure, observe and use modems.

* Auto-Connect: no

### mount-observe

Can query system mount information. This is restricted because it gives
privileged read access to mount arguments and should only be used with trusted
apps.

* Auto-Connect: no

### network-control

Can configure networking which gives wide, privileged access to networking.

* Auto-Connect: no

### network-manager

Can access snaps providing the network-manager interface which gives privileged
access to configure and observe networking.

* Auto-Connect: no

### network-observe

Can query network status information which gives privileged read-only access to
networking information.

* Auto-Connect: no

### ppp

Can access Point-to-Point protocol daemon which gives privileged access to
configure and observe PPP networking.

* Auto-Connect: no

### process-control

Can manage processes via signals and nice.

* Auto-Connect: no

### serial-port

Can access serial ports. This is restricted because it provides privileged
access to configure serial port hardware.

* Auto-Connect: no
* Attributes:
    * path (slot): path to serial device

### snapd-control

Can manage snaps via snapd.

* Auto-Connect: no

### system-observe

Can query system status information which gives privileged read access to all
processes on the system.

* Auto-Connect: no

### system-trace

Can use kernel tracing facilities. This is restricted because it gives
privileged access to all processes on the system and should only be used with
trusted apps.

* Auto-Connect: no

### timeserver-control

Can manage timeservers directly separate from ``config core``.

* Auto-Connect: no

### tpm

Can access the tpm device /dev/tpm0.

* Auto-Connect: no
