# Security policy

Most of the security aspects of the system will be done via interfaces,
slots and plugs. However for compatibility with the 15.04 snappy
architecture there is a special interface called `old-security`
that can be used to migrate using the 15.04 syntax. See the example
below for the various ways the `old-security` interface can be used.

## Security with the old-security interface

Snap packages run confined under a restrictive security sandbox by default.
The security policies and store policies work together to allow developers to
quickly update their applications and to provide safety to end users.

This document describes how to configure the security policies for snap
packages and builds upon the packaging declaration as defined in `meta.md`.

## How policy is applied
Application authors should not have to know about or understand the lowlevel
implementation details on how security policy is enforced. Instead, security
policy is typically defined by declaring a security template to use and any
additional security caps to extend the policy provided by the template. If
unspecified, default confinement allows the snap to run as a network client.

Applications are tracked by the system by using the concept of an
ApplicationId. The `APP_ID is` the composition of the package name, the app's
developer from the store if applicable -- only snaps of `type: app` (the
default) use an developer to compose the `APP_ID`), the
service/binary name and package version. The `APP_ID` takes the form of
`<pkgname>.<developer>_<appname>_<version>`. For example, if this is in
`snap.yaml`:

    name: foo
    version: 0.1
    ...
    apps:
      bar:
        start: bin/bar

and the app was uploaded to the `snapdev` developer in the store, then the
`APP_ID` for the `bar` service is `foo.snapdev_bar_0.1`. The `APP_ID` is used
throughout the system including in the enforcement of security policy by the
app launcher.

Under the hood, the launcher:

* sets up various environment variables (eg, `SNAP_ARCH`,
  `SNAP_DATA`, `SNAP`, `SNAP_USER_DATA`,
  `SNAP_OLD_PWD`, `HOME` and `TMPDIR`. See the
   [snappy FHS](https://developer.ubuntu.com/en/snappy/guides/filesystem-layout/) for details.
* sets up a device cgroup with default devices (eg, /dev/null, /dev/urandom,
  etc) and any devices which are assigned to this app via Gadget snaps or
  `snappy hw-assign` (eg, `snappy hw-assign foo.snapdev /dev/bar`).
* sets up the seccomp filter
* executes the app under an AppArmor profile under a default nice value

The launcher is used when launching both services and CLI binaries. The
security policy and launcher enforce application isolation as per the snappy
FHS.

This combination of restrictive AppArmor profiles (which mediate file access,
application execution, Linux capabilities(7), mount, ptrace, IPC, signals,
coarse-grained networking), clearly defined application-specific filesystem
areas, whitelist syscall filtering via seccomp and device cgroups provides for
strong application confinement and isolation (see below for future work).

### AppArmor
Upon snap package install, `snap.yaml` is examined and AppArmor profiles are
generated for each service and binary to have names based on the `APP_ID`.
As mentioned, AppArmor profiles are template based and may be extended through
policy groups, which are expressed in the yaml as `caps`.

### Seccomp
Upon snap package install, `snap.yaml` is examined and seccomp filters are
generated for each service and binary. Like with AppArmor, seccomp filters are
template based and may be extended through filter groups, which are expressed
in the yaml as `caps`.

## Defining snap policy

The `snap.yaml` need not specify anything for default confinement. Several
options are available in the `old-security` interface to modify the
confinement:

* `caps`: (optional) list of (easy to understand, human readable) additional
  security policies to add. The system will translate these to generate
  AppArmor and seccomp policy. Note: these are separate from `capabilities(7)`.
  Specify `caps: []` to indicate no additional `caps`.  When `caps` and
  `security-template` are not specified, `caps` defaults to client networking.
  Not compatible with `security-policy`.
    * AppArmor access is deny by default and apps are restricted to
      their app-specific directories, libraries, etc (enforcing ro,
      rw, etc).  Additional access beyond what is allowed by the
      declared `security-template` is declared via this option
    * seccomp is deny by default. Enough safe syscalls are allowed so
      that apps using the declared `security-template` should
      work. Additional access beyond what is allowed by the
      `security-template` is declared via this option
* `security-template`: (optional) alternate security template to use instead of
  `default`. When specified without `caps`, `caps` defaults to being empty. Not
  compatible with `security-policy`.
* `security-override`: (optional) overrides to use when `security-template` and
  `caps` are not sufficient. Not compatible with `security-policy`. The
  following keys are supported:
    * `read-paths`: a list of additional paths that the app can read
    * `write-paths`: a list of additional paths that the app can write
    * `abstractions`: a list of additional AppArmor abstractions for the app
    * `syscalls`: a list of additional syscalls that the app can use
* `security-policy`: (optional) hand-crafted low-level raw security policy to
  use instead of using default template-based security policy. Not compatible
  with `caps`, `security-template` or `security-override`.
    * `apparmor: path/to/profile`
    * `seccomp: path/to/filter`

Eg, consider the following:

    name: foo
    version: 1.0
    apps:
      bar:
        command: bar
      baz:
        command: baz
        slots: [baz-caps]
      qux:
        command: qux
        slots: [qux-security]
      quux:
        command: quux
        slots: [quux-policy]
      corge:
        command: corge
        daemon: simple
        slots: [corge-override]
      cli-exe:
        command: cli-exe
        slots: [no-caps]
    slots:
      baz-caps:
        type: old-security
        caps:
          - network-client
          - norf-framework_client
      qux-security:
        type: old-security
        security-template: nondefault
      quux-policy:
        type: old-security
        security-policy:
          apparmor: meta/quux.profile
          seccomp: meta/quux.filter
      corge-override:
        type: old-security
        security-override:
          apparmor: meta/corge.apparmor
          seccomp: meta/corge.seccomp
      no-caps:
        type: old-security
        caps: []


If this package is uploaded to the store in the `snapdev` developer, then:

* `APP_ID` for `bar` is `foo.snapdev_bar_1.0`. It uses the `default` template
  and `network-client` (default) cap
* `APP_ID` for `baz` is `foo.snapdev_baz_1.0`. It uses the `default` template
  and the `network-client` and `norf-framework_client` caps
* `APP_ID` for `qux` is `foo.snapdev_qux_1.0`. It uses the `nondefault`
  template and `network-client` (default) cap
* `APP_ID` for `quux` is `foo.snapdev_quux_1.0`. It does not use a
  `security-template` or `caps` but instead ships its own AppArmor policy in
  `meta/quux.profile`
  and seccomp filters in `meta/quux.filter`
* `APP_ID` for `corge` is `foo.snapdev_corge_1.0`. It does not use a
  `security-template` or `caps` but instead ships the override files
  `meta/corge.apparmor` and `meta/corge.seccomp`.
* `APP_ID` for `cli-exe` is `foo.snapdev_cli-exe_1.0`. It uses the `default`
  template and no `caps`

As mentioned, security policies and store policies work together to provide
flexibility, speed and safety. Use of some of the above will trigger a manual
review in the official Ubuntu store for snaps that are `type: app` (the
default):

* `security-policy` - always triggers a manual review because it allows
  specifying access beyond the application specific areas
* `caps` - will only trigger a manual review if specifying a `reserved` cap
* `security-template` - will only trigger a manual review if specifying a
  `reserved` tempate (eg, `unconfined`)
* `security-override` - will only trigger a manual review if specifying access
  beyond that provided by `common` access.

Apps should typically only use common groups with `caps` and common templates
with `security-template` and avoid `security-policy` and `security-override`.

Snaps that are of `type: framework` (see frameworks.md) will use any of the
above (since framework snaps' purpose is to extend the system and require
additional privilege).

The available templates and policy groups of the target system can be seen by
running `snappy-security list` on the target system.

## Debugging
To check to see if you have any denials:

    $ sudo grep audit /var/log/syslog

An AppArmor denial will look something like:

    audit: type=1400 audit(1431384420.408:319): apparmor="DENIED" operation="mkdir" profile="foo_bar_0.1" name="/var/lib/foo" pid=637 comm="bar" requested_mask="c" denied_mask="c" fsuid=0 ouid=0

If there are no AppArmor denials, AppArmor isn't blocking the app.

A seccomp denial will look something like:

    audit: type=1326 audit(1430766107.122:16): auid=1000 uid=1000 gid=1000 ses=15 pid=1491 comm="env" exe="/bin/bash" sig=31 arch=40000028 syscall=983045 compat=0 ip=0xb6fb0bd6 code=0x0

The `syscall=983045` can be resolved with the `scmp_sys_resolver` command:

    $ scmp_sys_resolver 983045
    set_tls

If there are no seccomp denials, seccomp isn't blocking the app.

For more information, please see
[debugging](https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement#Debugging).

## Future
The following is planned:

* launcher:
    * utilize syscall argument filtering
    * setup additional cgroups (tag network traffic, memory)
    * setup iptables using cgroup tags (for internal app access)
    * drop privileges to uid of service
* fine-grained network mediation via AppArmor
* `sockets`: (optional) `AF_UNIX` abstract socket definition for coordinated
  snap communications. Abstract sockets will be namespaced and yaml is such
  that (client) apps wanting to use the socket don't have to declare anything
  extra, but they don't have access unless the (server) binary declaring the
  socket says that app is ok).
    * `names`: (optional) list of abstract socket names
      (`<name>_<binaryname>` is prepended)
    * `allowed-clients`: `<name>.<developer>` or
     `<name>.<developer>_<binaryname>` (ie, omit version and
     `binaryname` to allow all from snap `<name>.<developer>` or omit
     version to allow only `binaryname` from snap `<name>`)

 Eg:

        name: foo
        ...
        services:
          - name: bar
          sockets:
            names:
              - sock1
              - sock2
              - ...
            allowed-clients:
              - baz
              - norf_qux
              - ...
