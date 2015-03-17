# Frameworks
## Definition
Frameworks are a direct extension of the Ubuntu Core. As such Frameworks have
the following attributes:

 * Frameworks exist primarily to provide mediation of shared resources (eg,
   device files, sensors, cameras, etc)
 * Frameworks are delivered via snaps
 * Frameworks can be installed on the same system without conflicts
 * Framework `binaries` may be used without prepending the package name
 * Frameworks run in a carefully crafted security profile
 * Frameworks are tightly coupled with separately maintained security policies
   that extend the security policy available to apps consuming a framework
 * Frameworks are developed and iterated by parties that have a contractual
   relationship with Canonical
 * Frameworks in the official Ubuntu store are designed jointly with Canonical
   as part of the framework onboarding process
 * Framework policies in the official Ubuntu store are controlled, designed,
   developed and uploaded to store by Canonical as part of that process
 * Framework policy will automatically be installed when a framework is
   installed
 * Unlike apps, frameworks have special permissions which allow them elevated
   access to the system. As such, the contract will include terms to ensure
   timely security updates and that the framework will not abuse this access

Note: snappy frameworks are somewhat different from the Ubuntu for Phones
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
   framework policy has not changed
 * For frameworks shipped in the official Ubuntu store, framework authors will
   enter a contract to provide any needed security updates and not be malicious

For the official Ubuntu Store, we will eventually allow separate ownership of
frameworks from framework policies which will allow framework authors to go
fast.

## Usage
### framework yaml

For frameworks, meta/packaging.yaml should contain something like:

    name: foo
    version: 1.1.234
    type: framework
    services:
      - name: bar
        description: "desc for bar service"
        start: bin/bar
    binaries:
      - name: bin/baz
        description: "desc for baz binary"
    security-policy:
      ...

Required fields for framework snaps:

 * `type: framework` - defines the type of snap this is
 * `security-policy`: TODO

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
package name be prepended. Eg, using the above package.yaml, either of these
may be used:

    $ foo.baz --version
    1.1.235

    $ baz --version
    1.1.235

