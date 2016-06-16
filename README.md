# Overview

The snap-confine program launches snappy applications to restrict
access. It uses apparmor and seccomp to do this.

Run with:

    $ snap-confine security-profile /path/to/binary additional args

Can run the tests with:

    $ make check

Note: the tests assume that seccomp denials are logged to dmesg. If seccomp is
killing processing without logging, verify that auditd is not installed.

## Apparmor

The apparmor part is similar to aa-exec -p, i.e. it will launch the application
under the given apparmor profile.


## Seccomp

The seccomp filter profile in expected to be located in
`/var/lib/snapd/seccomp/profiles`

The filter file contains lines with syscall names, comments that start
with "#" or special directives that start with a "@".

The supported special directives are:
@unrestricted

The unrestricted profile looks like this:
```
# Unrestricted profile
@unrestricted
```

A very strict profile might look like this:
```
# Super strict profile
read
write
```


## devices cgroup

It works like this:
- on install of snaps with a special hardware: assign yaml udev rules are
  generated that add tags to matching hardware. These assign rules are added to
  udev via /etc/udev/rules.d/70-snap.... for each app within a snap. The tags
  are of the form 'snap_<snap name>_<app>'.
- when an application is launched, the launcher queries udev to detect if any
  devices are tagged for this application. If no devices are tagged for this
  application, a device cgroup is not setup
- if there are devices tagged for this application, the launcher creates a
  device cgroup in /sys/fs/cgroups/devices/snap.<snap name>.<app> and adds
  itself to this cgroup. It then sets the cgroup as deny-all by default, adds
  some common devices (eg, /dev/null, /dev/zero, etc) and any devices tagged
  for use by this application using /lib/udev/snappy-app-dev
- the app is executed and now the normal device permissions/apparmor rules
  apply
- udev match rules in /lib/udev/rules.d/80-snapy-assign.rules are in place to
  run /lib/udev/snappy-app-dev to handle device events for devices tagged with
  snap_*.

Note, /sys/fs/cgroups/devices/snap.<snap name>.<app> is not (currently) removed
on unassignment and the contents of the cgroup for the app are managed entirely
by the launcher. When an application is started, the cgroup is reset by
removing all previously added devices and then the list of assigned devices is
built back up before launch. In this manner, devices can be assigned, changed,
and unassigned and the app will always get the correct device added to the
cgroup, but what is in /sys/fs/cgroups/devices/snap.<snap name>.<app> will not
reflect assignment/unassignment until after the application is started.


## private /tmp

The launcher will create a private mount namespace for the application and
mount a per-app /tmp directory under it.


## devpts newinstance

The launcher will setup a new instance devpts for each application.
