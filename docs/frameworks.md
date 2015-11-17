# Frameworks
## Definition
Frameworks are a direct extension of the Ubuntu Core. As such frameworks have
the following attributes:

* Frameworks exist primarily to provide mediation of shared resources (eg,
  device files, sensors, cameras, etc)
* Frameworks provide a significant benefit for many users
* Frameworks are delivered via snaps
* Frameworks can be installed on the same system without conflicts
* Frameworks are unique and multiple origins for a framework are not supported
  (therefore frameworks must always be referred to without an `origin`)
* Framework `binaries` may be used without appending the package name and
  these binary names are governed by the framework onboarding process (below)
* Frameworks must not depend on other frameworks
* Frameworks run in a carefully crafted security profile
* Frameworks are tightly coupled with separately maintained security policies
  that extend the security policy available to apps consuming a framework
* Frameworks are developed and iterated by parties that have a contractual
  relationship with Canonical
* Frameworks in the official Ubuntu store are designed jointly with Canonical
  as part of the framework onboarding process
* Framework policies in the official Ubuntu store are controlled, designed,
  and developed by Canonical with the framework authors as part of that process
* Framework policy will automatically be installed when a framework is
  installed
* Unlike apps, frameworks have special permissions which allow them elevated
  access to the system. For the official Ubuntu store, a contract will include
  terms to ensure framework safety and maintenance

Importantly, frameworks are not generally:

* used as a replacement mechanism for debs/rpms
* used as a method to share code (ie, don't create a framework with libraries
  just for the sake of apps to be able to use them)
* used as a method to bypass app isolation
* able to be forked (a user may of course always sideload a modified framework)

*Note:* snappy frameworks are somewhat different from the Ubuntu for Phones
[click frameworks](https://wiki.ubuntu.com/Click/Frameworks) and are more
flexible. Most importantly, click frameworks for Ubuntu for Phones map to a
particular release and are contracts between the platform (OS) and apps. Snappy
splits out the platform (OS) and the framework such that the contract is split
between the `framework` and the platform `release` (OS) (the `release` and
installed `frameworks` can be seen via `snappy info`). As such, apps will
specify the release they target (implementation covered elsewhere) and any
frameworks they require.

## Store process
Initially, frameworks and framework policy will be shipped in the same snap
which will ensure that framework policy is always in sync with the framework
for which it applies. To support this:

* Frameworks must always specify framework policy, otherwise the store will
  reject it
* Framework snaps will always trigger a manual review to ensure the
  framework policy has not changed. Alternatively, the first upload could
  require manual review, but subsequent uploads could be automatically
  approved if the security policy does not change
* For frameworks shipped in the official Ubuntu store, framework authors will
  enter a contract to provide any needed security updates and not be malicious

For the official Ubuntu Store, we may eventually allow separate ownership of
frameworks from framework policies.

## Usage
### framework yaml

For frameworks, meta/package.yaml might contain something like:

    name: foo
    version: 1.1.234
    type: framework
    services:
      - name: bar
        description: "desc for bar service"
        start: bin/bar
        bus-name: com.example.foo
    binaries:
      - name: bin/baz
        description: "desc for baz binary"

Required fields for framework snaps:

* `type: framework` - defines the type of snap this is

#### DBus connection name
For framework services that provide a DBus interface, use `bus-name` to specify
the DBus connection name the service will bind to on the system bus (only
`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[A-Za-z0-9][A-Za-z0-9_-]*)+$` is allowed). To
preserve coinstallability, the `bus-name` should typically use the form of one
of the following: `<name>`, `<name>.<service name>`, `<reverse domain>.<name>`
or `<reverse domain>.<name>.<service name>`. In the above yaml, any of the
following can be used:

* `bus-name: foo`
* `bus-name: foo.bar`
* `bus-name: com.example.foo`
* `bus-name: com.example.foo.bar`

#### Security policy
Frameworks will typically need specialized security policy. See `security.md`
for details.

In addition to the above yaml fields, the security policy used by apps is
shipped in the `meta/framework-policy` directory according to the following
hierarchy:

* `meta/framework-policy/`
    * `apparmor/`
        * `policygroups/`
            * `group1`
            * `group2`
        * `templates/`
            * `template1`
            * `template2`

Because frameworks must be coinstallable, all shipped policy files will be
prepended with the framework name followed by an underscore. Apps must
reference the policy using the full name. For example, if the `foo` framework
ships `meta/framework-policy/apparmor/policygroups/bar-client`, then apps must
reference this as `foo_bar-client`.

While the above provides a lot of flexibility, it is important to remember a
framework snap need only provide what apps will use. For example, if the `foo`
framework is designed to have clients connect to the `bar` service over DBus,
then the framework snap might provide
`meta/framework-policy/apparmor/policygroups/foo_bar-client` and nothing else.

The contents of files in the `apparmor` directory use apparmor syntax as
described in `apparmor.d(5)`. When specifying DBus rules, set the peer label to
refer to the AppArmor label (`APP_ID`) of the service to be accessed. Also, to
ensure frameworks are coinstallable, the service should be implemented so its
DBus `path` uses the format `/pkgname/service`.

For example, using the above example where the `foo` framework ships a `bar`
DBus system service, a `bin/exe` utility, some data files and also a runtime
state file, then `meta/framework-policy/apparmor/policygroups/bar-client`
might contain something like:

    /apps/foo/*/bin/exe  ixr,
    /apps/foo/*/data/** r,
    /var/lib/apps/foo/*/run/state r,
    dbus (receive, send)
         bus=system
         peer=(label=foo_bar_*),

### App yaml

For apps wanting to use a particular framework, meta/package.yaml simply
references the security policy provided by the framework. Eg, if a service in
the `norf` app wants to access the `bar` service provided by the `foo`
framework in the above framework yaml example, it might use:

    name: norf
    version: 2.3
    frameworks:
      - foo
    services:
      - name: qux
        description: "desc for qux service"
        start: bin/qux
        caps:
          - network-client
          - foo_bar-client

See `security.md` for more information on specifying `caps` and a
`security-template` as provided by the framework snap.

### User experience

The command line experience is:

    $ snappy search foo
    Name      Version      Description
    foo       1.1.234      The foo framework

    $ snappy install foo
    Installing foo
    Starting download of foo
    4.03 MB / 4.03 MB [==============================] 100.00 % 124.66 KB/s
    Done
    Name                 Date       Version   Summary
    foo                  2015-03-16 1.1.234   The foo framework

    $ snappy list
    Name                 Date       Version   Summary
    ubuntu-core          2015-03-16 333       ubuntu-core description
    foo                  2015-03-16 1.1.234   The foo framework
    hello-world          2015-02-23 1.0.5

    $ snappy list --updates
    Name                  Date      Version
    ubuntu-core          2015-03-16 333      ubuntu-core description
    foo*                 2015-03-16 1.1.234  The foo framework
    hello-world          2015-02-23 1.0.5

    $ sudo snappy update
    Installing foo (1.1.235)
    4.03 MB / 4.03 MB [==============================] 100.00 % 124.66 KB/s
    Done
    Name                 Date       Version   Summary
    foo                  2015-03-17 1.1.235   The foo framework

    $ snappy list --updates
    Name                  Date      Version
    ubuntu-core          2015-03-16 333      ubuntu-core description
    foo                  2015-03-17 1.1.235  The foo framework
    hello-world          2015-02-23 1.0.5

    $ snappy remove foo
    Removing foo

    $ snappy info
    release: ubuntu-core/devel-proposed
    architecture: amd64
    frameworks: foo
    apps: hello-world

A convenience afforded to frameworks is that commands don't require that the
package name be appended. Eg, using the above `package.yaml`, use:

    $ baz --version
    1.1.235


## Open questions

The following are considerations that may affect the above for when we build on
this work:

* define how to specify restricted security policy (perhaps simply refine
  what we do on Touch with meta information contained in the policy)
* define how to allow certain apps to use restricted policy without manual
  review (perhaps have the framework define which apps are allowed to use the
  restricted policy. How? in the yaml? in the meta information in the policy?
  Something in the `meta/framework-policy` directory?)
* should we adjust `hw-assign/create svc-assign` to support special framework
  services that perhaps don't provide sufficient app isolation, are privileged
  in some manner, etc? Eg, consider a DBus service that allows you to
  configure network interfaces. Framework provides the `bar-srv` service and
  app `baz-app` declares it wants to use that service via `caps`. In the
  normal case, declaring in `caps` would be enough, but `bar-srv` is special
  in some way that we don't want the app to have access automatically. In this
  case we might use:

       `snappy svc-assign baz-app bar-srv`

* if we implement this, how should we declare `bar-srv` access to `bar-srv` is
  restricted in this manner?
* should we allow users the ability to to use the `binaries` with the appended
  package name? Eg::

    	$ baz.foo --version
    	1.1.235
* ...
