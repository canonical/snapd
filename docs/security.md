# Security policy

Snap packages run confined under a restrictive security sandbox by default.
The security policies and store policies work together to allow developers to
quickly update their applications and to provide safety to end users.

This document describes how to configure the security policies for snap
packages and builds upon the packaging declaration as defined in `meta.md`.

## How policy is applied
Security policy is typically defined by declaring a security template and any
additional security groups to extend the policy provided by the template. If
unspecified, default confinement allows the snap to run as a network client.

Applications are tracked by the system by using the concept of an
ApplicationId. The `APP_ID is` the composition of the package name, the
service/binary name and package version. The `APP_ID` takes the form of
`<pkgname>_<appname>_<version>`. For example, if this is in `package.yaml`:

    name: foo
    version: 0.1
    ...
    services:
      - name: bar
        start: bin/bar

Then the `APP_ID` for the `bar` service is `foo_bar_0.1`. The `APP_ID` is used
throughout the system including in the enforcement of security policy by the
app launcher. The launcher will:

* setup various environment variables (eg, `SNAP_APP_ARCH`,
  `SNAP_APP_DATA_PATH`, `SNAP_APP_PATH`, `SNAP_APP_TMPDIR`,
  `SNAP_APP_USER_DATA_PATH`, `SNAP_OLD_PWD`, `HOME` and `TMPDIR` (set to
  `SNAP_APP_TMPDIR`). See the
   [snappy FHS](https://developer.ubuntu.com/en/snappy/guides/filesystem-layout/) for details.
* chdir to `SNAP_APP_PATH` (the install directory)
* setup the seccomp filter
* exec the app under AppArmor profile under a default nice value

The launcher will be used when launching both services and when using CLI
binaries. The launcher enforces application isolation as per the snappy FHS.

### AppArmor
Upon snap package install, `package.yaml` is examined and AppArmor profiles are
generated for each service and binary to have names based on the `APP_ID`.
As mentioned, AppArmor profiles are template based and may be extended through
policy groups, which are expressed in the yaml as `caps`.

### Seccomp
Upon snap package install, `package.yaml` is examined and seccomp filters are
generated for each service and binary. As mentioned, seccomp filters are
template based and may be extended through filter groups, which are expressed
in the yaml as `caps`.

## Defining snap policy

The `package.yaml` need not specify anything for default confinement. Several
options are available to modify the confinement:

* `caps`: (optional) list of (easy to understand, human readable) additional
  security policies to add. The system will translate these to generate
  AppArmor and seccomp policy. Note: these are separate from `capabilities(7)`.
  When `caps` and `security-template` are not specified, defaults to client
  networking. Not compatible with `security-override` or `security-policy`.
 * AppArmor access is deny by default and apps are restricted to their
   app-specific directories, libraries, etc (enforcing ro, rw, etc).
   Additional access beyond what is allowed by the declared `security-template`
   is declared via this option
 * seccomp is deny by default. Enough safe syscalls are allowed so that apps
   using the declared `security-template` should work. Additional access
   beyond what is allowed by the `security-template` is declared via this
   option
* `security-template`: (optional) alternate security template to use instead of
  `default`. When specified, `caps` defaults to empty list. Not compatible with
  `security-override` or `security-policy`.
* `security-override`: (optional) high level overrides to use when
  `security-template` and `caps` are not sufficient - see
  [Advanced usage](https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement)
  for details. Not compatible with `caps`, `security-template` or
  `security-policy`
 * `apparmor: path/to/security override`
 * `seccomp: path/to/filter override`
* `security-policy`: (optional) hand-crafted low-level raw security policy to
  use instead of using default template-based security policy. Not compatible
  with `caps`, `security-template` or `security-override`.
 * `apparmor: path/to/profile`
 * `seccomp: path/to/filter`

Eg, consider the following:

    name: foo
    version: 1.0
    services:
      - name: bar
      - name: baz
        caps:
          - network-client
          - norf-framework_client
      - name: qux
        security-template: nondefault
      - name: quux
        security-policy:
          apparmor: meta/quux.profile
          seccomp: meta/quux.filter
      - name: corge
        security-override:
          apparmor: meta/corge.apparmor
          seccomp: meta/corge.seccomp
    binaries:
      - name: cli-exe
        caps: none

With the above:

* `APP_ID` for `bar` is `foo_bar_1.0`. It uses the `default` template and
  `network-client` (default) cap
* `APP_ID` for `baz` is `foo_baz_1.0`. It uses the `default` template and the
  `network-client` and `norf-framework_client` caps
* `APP_ID` for `qux` is `foo_qux_1.0`. It uses the `nondefault` template and
  `network-client` (default) cap
* `APP_ID` for `quux` is `foo_quux_1.0`. It does not use a `security-template`
  or `caps` but instead ships its own AppArmor policy in `meta/quux.profile`
  and seccomp filters in `meta/quux.filter`
* `APP_ID` for `corge` is `foo_corge_1.0`. It does not use a
  `security-template` or `caps` but instead ships the override files
  `meta/corge.apparmor` and `meta/corge.seccomp`.
* `APP_ID` for `cli-exe` is `foo_cli-exe_1.0`. It uses the `default` template
  and no `caps`

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

## Future
The following is planned:

* launcher:
 * setup cgroups (tag network traffic, block devices, memory)
 * setup iptables using cgroup tags (for internal app access)
 * drop privileges to uid of service
* `sockets`: (optional) `AF_UNIX` abstract socket definition for coordinated
  snap communications. Abstract sockets will be namespaced and yaml is such
  that (client) apps wanting to use the socket don't have to declare anything
  extra, but they don't have access unless the (server) binary declaring the
  socket says that app is ok).
 * `names`: (optional) list of abstract socket names (`<name>_<binaryname>` is
   prepended)
 * `allowed-clients`: `<name>` or `<name>_<binaryname>` (ie, omit
   version and `binaryname` to allow all from snap `<name>` or omit version
   to allow only `binaryname` from snap `<name>`)

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
