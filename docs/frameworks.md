# Frameworks
## Definition
Frameworks are a direct extension of the Ubuntu Core. As such Frameworks have
the following attributes:

 * Frameworks exist primarily to provide mediation of shared resources (eg,
   device files, sensors, cameras, etc)
 * Frameworks are delivered via snaps
 * Frameworks can be installed on the same system without conflicts
 * Frameworks run in a carefully crafted security profile
 * Frameworks are tightly coupled with separately maintained security policies
   that extend the security policy available to apps consuming a framework
 * Frameworks are developed and iterated by parties that have a contractual
   relationship with Canonical
 * Frameworks are designed jointly with Canonical as part of the framework
   onboarding process
 * Framework policies are controlled, designed, developed and uploaded to store
   by Canonical as part of that process
 * Framework policy will automatically be installed when a framework is installed
 * Unlike apps, frameworks have special permissions which allow them elevated
   access to the system. As such, the contract will include terms to ensure
   timely security updates and that the framework will not abuse this access

## Store process
We want to allow framework authors to go fast and allow separate ownership of
frameworks from framework policies. Therefore, we split the security policy
from the framework package itself and introduce two separate snap package
types: framework and framework-policy.

To support the above definition:

 * Frameworks cannot be uploaded to the store without the corresponding
   framework-policy already being in the store (the store will automatically
   reject it)
 * Framework policy snaps always trigger a manual review (ensuring we can
   verify their origin)
 * Framework authors will enter a contract to provide any needed security
   updates and not be malicious

In this manner, the store enforces that the framework author works with
Canonical to ensure the initial implementation is secure, then after that,
policy will be uploaded to the store for that particular framework, after which
the framework author is free to iterate on the framework unencumbered.

When searching the store, the framework name is sent to the store as part of
the installed frameworks http request (e.g. if 'name: foo' then the store
will see 'frameworks: foo' in the http request).

## Usage
### framework yaml

For frameworks, meta/packaging.yaml should contain something like:

    name: foo
    version: 1.1.234
    type: framework
    frameworks: foo-policy
    services:
      - name: bar
        description: "desc for bar service"
        start: bin/bar

The framework yaml does not reference the framework, snappy will query the
store for the associated framework policy and `snappy install` will install the
policy at the same time as the framework.

ALTERNATIVE: instead of specifying `frameworks: foo-policy`, snappy could
query the store for the associated framework policy and `snappy install` would
install the policy at the same time as the framework.

Required fields for framework snaps:

 * `type: framework` - defines the type of snap this is
 * `frameworks: <name>-policy` - the framework policy that corresponds with
   this snap


### TODO: framework-policy yaml

For framework policy, meta/packaging.yaml should contain something like:

    name: foo-policy
    version: 1.1.0
    type: framework-policy
    target: foo
    security-policy: somedir
    profiles:
      - name: bar
        foo: meta/foo-daemon.profile




Required fields for framework policies
TODO


### User experience

In general:

 * user should not have to care or even know about security policy when
   installing frameworks (ie, should not show up in search, should be installed
   with framework)
 * user should know when there are policy updates

The command line experience is:

    $ snappy search foo
    Name      Version      Description
    foo       1.1.234      The foo framework

    $ snappy install foo
    Installing foo-policy
    Starting download of foo-policy
    0.07 MB / 0.07 MB [==============================] 100.00 % 122.16 KB/s
    Done
    Installing foo
    Starting download of foo
    4.03 MB / 4.03 MB [==============================] 100.00 % 124.66 KB/s
    Done
    Name                 Date       Version   Summary
    foo-policy           2015-03-16 1.1.0     The foo framework policy
    foo                  2015-03-16 1.1.234   The foo framework

    $ snappy list
    Name                 Date       Version   Summary
    ubuntu-core          2015-03-16 333       ubuntu-core description
    foo-policy           2015-03-16 1.1.0     The foo framework policy
    foo                  2015-03-16 1.1.234   The foo framework
    hello-world          2015-02-23 1.0.5

    $ snappy list --updates
    Name                  Date      Version
    ubuntu-core          2015-03-16 333      ubuntu-core description
    foo-policy           2015-03-16 1.1.0    The foo framework policy
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
    foo-policy           2015-03-16 1.1.0    The foo framework policy
    foo                  2015-03-17 1.1.235  The foo framework
    hello-world          2015-02-23 1.0.5

    $ snappy remove foo-policy
    foo-policy cannot be removed while foo is installed.

    $ snappy remove foo
    Removing foo
    Removing foo-policy

_(removes both foo and foo-policy)_

    $ snappy info
    release: ubuntu-core/devel-proposed
    architecture: amd64
    frameworks: foo
    apps: hello-world

    $ snappy info --verbose
    release: ubuntu-core/devel-proposed
    architecture: amd64
    frameworks: foo
    framework-policies: foo-policy
    apps: hello-world

