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
 * explicit-license-agreemen: set to "Y" if the user needs to accept a
   special meta/license.txt before the snap can be installed
 
 * type: the type of the snap, can be:
   * app - the default
   * oem - a special snap that OEMs can use to customize snappy for
           their hardware
   * framework - a special snap with more power, see frameworks.md

 * architectures: a yaml list of supported architectures
 * framework: the frameworks the snap needs as dependencies

 * services: the servies (daemons) that the snap provides
 * name: name of the service
   * description: description of the service
   * start: the command to start the service
   * stop: the command to stop the service
   * stop-timeout: the time in seconds to wait for the service to stop
   * poststop: a command that runs after the service has stopped

 * binaries: the binaries (executables) that the snap provies
   * name: the name of the binary, the user will be able to call it
           as $name.$pkgname
   * exec: the program that gets execute (can be omited if name points
           to a binary already)
   * security-template: the security template to use (can be omitted,
                        the default template is used in this case
   * security-policy: the special security policy to use (can be omitted)
 
## license.txt

A license text that the user must accepted before the snap can be
installed.


## hooks/ directory

See config.md for details.

# Examples

See lp:~snappy-dev/snappy-hub/snappy-examples for up-to-date examples.
