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
the installed frameworks http request (e.g. if 'name: docker' then the store
will see 'frameworks: docker' in the http request).

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
        docker: meta/docker-daemon.profile




Required fields for framework policies
TODO
