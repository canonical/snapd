# Package metadata

This document describes the meta data of a snappy package. All files
are located under the `meta/` directory.

The following files are supported:

## readme.md

The `readme.md` file contains a description of the snap. The snappy
tools will automatically extract the heading as the short summary for
the snap and the first paragraph as the description in the store.

## package.yaml

This file describes the snap package and is the most important file
for a snap package. The following keys are mandatory:

* `name`: the name of the snap (only `[a-z0-9][a-z0-9+-]`)
* `version`: the version of the snap (only `[a-zA-Z0-9.+~-]` are allowed)
* `vendor`: the vendor of the snap

The following keys are optional:

* `icon`: a SVG icon for the snap that is displayed in the store
* `explicit-license-agreement`: set to `Y` if the user needs to accept a
  special `meta/license.txt` before the snap can be installed
* `license-version`: a string that, when it changes and
  `explicit-license-agreement` is `Y`, prompts the user to accept the
  license again.
* `type`: (optional) the type of the snap, can be:
    * `app` - the default if empty
    * `oem` - a special snap that OEMs can use to customize snappy for
            their hardware
    * `framework` - a specialized snap that extends the system that other
                  snaps may use

* `architectures`: (optional) a yaml list of supported architectures
                   `["all"]` if empty
* `frameworks`: a list of the frameworks the snap needs as dependencies

* `services`: the servies (daemons) that the snap provides
    * `name`: (required) name of the service (only `[a-zA-Z0-9+.-]`)
    * `description`: (required) description of the service
    * `start`: (required) the command to start the service
    * `stop`: (optional) the command to stop the service
    * `stop-timeout`: (optional) the time in seconds to wait for the
                      service to stop
    * `poststop`: (optional) a command that runs after the service has stopped
    * `forking`: (optional) set to "true" if the service calls fork() as
                 part of its startup
    * `caps`: (optional) list of additional security policies to add.
              See `security.md` for details
    * `security-template`: (optional) alternate security template to use
                           instead of `default`. See `security.md` for details
    * `security-override`: (optional) high level overrides to use when
                           `security-template` and `caps` are not
                           sufficient.  See security.md for details
    * `security-policy`: (optional) hand-crafted low-level raw security
                         policy to use instead of using default
                         template-based  security policy. See
                         security.md for details
    * `ports`: (optional) define what ports the service will work
        * `internal`: the ports the service is going to connect to
            * `tagname`: a free form name
                * `port`: (optional) number/protocol, e.g. `80/tcp`
                * `negotiable`: (optional) Y if the app can use a different port
        * `external`: the ports the service offer to the world
            * `tagname`: a free form name, some names have meaning like "ui"
                * `port`: (optional) see above
                * `negotiable`: (optional) see above
    * `bus-name`: (optional) message bus connection name for the service.
      May only be specified for snaps of 'type: framework' (see above). See
      frameworks.md for details.
    * `socket`: (optional) Set to "true" if the service is socket activated.
                Must be specified with `listen-stream`.
    * `listen-stream`: (optional) The full path of the stream socket or an
                abstract socket. When specifying an absolute path, it should
                normally be in one of the app-specific writable directories.
                When specifying an abstract socket, it must start with '@' and
                typically be followed by either the snap package name or the
                snap package name followed by '\_' and any other characters
                (eg, '@name' or '@name\_something').
    * `socket-user`: (optional) The user that owns the stream socket. The user
                     should normally match the snap package name. Must be
                     specified with `listen-stream`. This option is reserved
                     for future use.
    * `socket-group`: (optional) The group that own the stream socket. The
                      group should normally match the snap package name. Must
                      be specified with `listen-stream`. This option is
                      reserved for future use.

* `binaries`: the binaries (executables) that the snap provides
    * `name`: (required) the name of the binary, the user will be able to
              call it as $name.$pkgname (only `[a-zA-Z0-9+.-]`)
    * `exec`: the program that gets executed (can be omited if name points
              to a binary already)
    * `caps`: (optional) see entry in `services` (above)
    * `security-template`: (optional) see entry in `services` (above)
    * `security-override`: (optional) see entry in `services` (above)
    * `security-policy`: (optional) see entry in `services` (above)

## license.txt

A license text that the user must accept before the snap can be
installed.

## hooks/ directory

See `config.md` for details.

# Examples

See https://github.com/ubuntu-core/snappy-testdata for up-to-date examples.
