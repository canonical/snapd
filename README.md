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
under the specified apparmor profile.


## Seccomp

The seccomp filter profile in expected to be located in
`/var/lib/snapd/seccomp/profiles`

The filter file contains lines with syscall names, comments that start with "#"
or special directives that start with a "@". Lines with syscall names may
optionally specify additional arguments. Eg:

        RULE = ( <syscall> [ARGS] | DIRECTIVE )

        DIRECTIVE = @unrestricted

        ARGS = ( - | [CONDITIONAL]VALUE )*

        CONDITIONAL = ( '!', '>', '>=', '<', '<=' )

        VALUE = ( UNSIGNED INT | KEY )

        KEY = ( SOCKET DOMAIN | SOCKET TYPE | PRCTL | PRIO )

        SOCKET DOMAIN = ( AF_UNIX | AF_LOCAL | AF_INET | AF_INET6 | AF_IPX |
        AF_NETLINK | AF_X25 | AF_AX25 | AF_ATMPVC | AF_APPLETALK | AF_PACKET |
        AF_ALG | AF_CAN )

        SOCKET TYPE = ( SOCK_STREAM | SOCK_DGRAM | SOCK_SEQPACKET | SOCK_RAW |
        SOCK_RDM | SOCK_PACKET )

        PRCTL = ( PR_CAP_AMBIENT | PR_CAP_AMBIENT_RAISE |
        PR_CAP_AMBIENT_LOWER | PR_CAP_AMBIENT_IS_SET |
        PR_CAP_AMBIENT_CLEAR_ALL | PR_CAPBSET_READ | PR_CAPBSET_DROP |
        PR_SET_CHILD_SUBREAPER | PR_GET_CHILD_SUBREAPER | PR_SET_DUMPABLE |
        PR_GET_DUMPABLE | PR_SET_ENDIAN | PR_GET_ENDIAN | PR_SET_FPEMU |
        PR_GET_FPEMU | PR_SET_FPEXC | PR_GET_FPEXC | PR_SET_KEEPCAPS |
        PR_GET_KEEPCAPS | PR_MCE_KILL | PR_MCE_KILL_GET | PR_SET_MM |
        PR_SET_MM_START_CODE | PR_SET_MM_END_CODE | PR_SET_MM_START_DATA |
        PR_SET_MM_END_DATA | PR_SET_MM_START_STACK | PR_SET_MM_START_BRK |
        PR_SET_MM_BRK | PR_SET_MM_ARG_START | PR_SET_MM_ARG_END |
        PR_SET_MM_ENV_START | PR_SET_MM_ENV_END | PR_SET_MM_AUXV |
        PR_SET_MM_EXE_FILE | PR_MPX_ENABLE_MANAGEMENT |
        PR_MPX_DISABLE_MANAGEMENT | PR_SET_NAME | PR_GET_NAME |
        PR_SET_NO_NEW_PRIVS | PR_GET_NO_NEW_PRIVS | PR_SET_PDEATHSIG |
        PR_GET_PDEATHSIG | PR_SET_PTRACER | PR_SET_SECCOMP | PR_GET_SECCOMP |
        PR_SET_SECUREBITS | PR_GET_SECUREBITS | PR_SET_THP_DISABLE |
        PR_TASK_PERF_EVENTS_DISABLE | PR_TASK_PERF_EVENTS_ENABLE |
        PR_GET_THP_DISABLE | PR_GET_TID_ADDRESS | PR_SET_TIMERSLACK |
        PR_GET_TIMERSLACK | PR_SET_TIMING | PR_GET_TIMING | PR_SET_TSC |
        PR_GET_TSC | PR_SET_UNALIGN | PR_GET_UNALIGN )

        PRIO = ( PRIO_PROCESS | PRIO_PGRP | PRIO_USER )

See `man 2 socket` and `man 2 prctl` for details on `SOCKET DOMAIN`,
`SOCKET TYPE` and `PRCTL`.

Specifying '-' as the argument skips filtering for that argument. Not
specifying a conditional mean exact match. The syntax is meant to reflect
how `seccomp_rule_add(3)` is used.

Examples:

* The unrestricted profile looks like this:

        # Unrestricted profile
        @unrestricted

* A very strict profile might look like this:

        # Super strict profile
        read
        write

* Use of seccomp argument filtering:

        # allow any socket types for AF_UNIX and AF_LOCAL
        socket AF_UNIX
        socket AF_LOCAL

        # Only allow SOCK_STREAM and SOCK_DGRAM for AF_INET
        socket AF_INET SOCK_STREAM
        socket AF_INET SOCK_DGRAM

        # Allow renicing of one's own process (arg2 is '0) to higher nice values
        setpriority - 0 >=0

        # Allow dropping privileges to uid/gid '1' and raising back again
        setuid <=1
        setgid <=1
        seteuid <=1
        setegid <=1

Limitations
 * seccomp argument filtering only allows specifying positive integers as
   arguments which means you may not dereference pointers, etc.
 * up to 6 arguments may be specified


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
