# Package metadata

This document describes the meta data of a snappy package. All files
are located under the meta/ directory. 

The following files are supported:

## readme.md

The readme.md file contains a description of the snap. The snappy
tools will automatically extract the heading as the short summary for
the snap and the first paragraph as the description in the store.

## package.yaml

This file describes the snap package and is the most important file
for a snap package. The following keys are mandatory:

 * name: the name of the snap
 * version: the version of the snap
 * vendor: the vendor of the snap

The following keys are optional:
 * icon: a svg icon for the snap that is displayed in the store
 * explicit-license-agreement: set to "Y" if the user needs to accept a
   special meta/license.txt before the snap can be installed
 
 * type: (optional) the type of the snap, can be:
   * app - the default if empty
   * oem - a special snap that OEMs can use to customize snappy for
           their hardware
   * framework - a specialized snap that extends the system that other
                 snaps may use

 * architectures: (optional) a yaml list of supported architectures
                  ["all"] if empty
 * framework: the frameworks the snap needs as dependencies

 * services: the servies (daemons) that the snap provides
   * name: (required) name of the service
   * description: (required) description of the service
   * start: (required) the command to start the service
   * stop: (optional) the command to stop the service
   * stop-timeout: (optional) the time in seconds to wait for the
                   service to stop
   * poststop: a command that runs after the service has stopped
   * caps: (optional) list of additional security policies to add.
           See security.md for details
   * security-template: (optional) alternate security template to use
                        instead of `default`. See security.md for details 
   * security-override: (optional) high level overrides to use when
                        `security-template` and `caps` are not
                        sufficient.  See security.md for details
   * security-policy: (optional) hand-crafted low-level raw security
                      policy to use instead of using default
                      template-based  security policy. See
                      security.md for details
   * ports: (optional) define what ports the service will work
     * internal: the ports the service is going to connect to
       * tagname: a free form name
         * port: (optional) number/protocol, e.g. 80/tcp
         * negotiable: (optional) Y if the app can use a different port
     * external: the ports the service offer to the world
       * tagname: a free form name, some names have meaning like "ui"
         * port: (optional) see above
         * negotionalble: (optional) see above
 
 * binaries: the binaries (executables) that the snap provies
   * name: the name of the binary, the user will be able to call it
           as $name.$pkgname (required)
   * exec: the program that gets execute (can be omited if name points
           to a binary already)
   * caps: (optional) see entry in services (above)
   * security-template: (optional) see entry in services (above)
   * security-override: (optional) see entry in services (above)
   * security-policy: (optional) see entry in services (above)   
 
## license.txt

A license text that the user must accept before the snap can be
installed.

## hooks/ directory

See config.md for details.

# Examples

See lp:~snappy-dev/snappy-hub/snappy-examples for up-to-date examples.
