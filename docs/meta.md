# Package metadata

This document describes the meta data of a snappy package. All files
are located under the `meta/` directory.

The following files are supported:

## snap.yaml

This file describes the snap package and is the most important file
for a snap package. The following keys are mandatory:

* `name`: the name of the snap (only `^[a-z](?:-?[a-z0-9])*$`)
* `version`: the version of the snap (only `[a-zA-Z0-9.+~-]` are allowed)

The following keys are optional:

* `summary`: A short summary
* `description`: A long description
* `license-agreement`: set to `explicit` if the user needs to accept a
  special `meta/license.txt` before the snap can be installed
* `license-version`: a string that, when it changes and
  `license-agreement` is `explicit`, prompts the user to accept the
  license again.
* `type`: (optional) the type of the snap, can be:
    * `app` - the default if empty
    * `gadget` - a special snap that Gadgets can use to customize snappy for
            their hardware
    * `framework` - a specialized snap that extends the system that other
                  snaps may use

* `architectures`: (optional) a yaml list of supported architectures
                   `["all"]` if empty
* `frameworks`: a list of the frameworks the snap needs as dependencies

* `apps`: the map of apps (binaries and services) that a snap provides
    * `command`: (required) the command to start the service
    * `daemon`: (optional) [simple|forking|oneshot|dbus]
    * `stop-command`: (optional) the command to stop the service
    * `stop-timeout`: (optional) the time in seconds to wait for the
                      service to stop
    * `restart-condition`: (optional) if specified, use the given restart
      condition. Can be one of `on-failure` (default), `never`, `on-success`,
      `on-abnormal`, `on-abort`, and `always`. See `systemd.service(5)`
      (search for `Restart=`) for details.
    * `post-stop-command`: (optional) a command that runs after the service
                          has stopped
    * `slots`: a map of interfaces
    * `ports`: (optional) define what ports the service will work
        * `internal`: the ports the service is going to connect to
            * `tagname`: a free form name
                * `port`: (optional) number/protocol, e.g. `80/tcp`
                * `negotiable`: (optional) Y if the app can use a different port
        * `external`: the ports the service offer to the world
            * `tagname`: a free form name, some names have meaning like "ui"
                * `port`: (optional) see above
                * `negotiable`: (optional) see above
    * `socket`: (optional) Set to "true" if the service is socket activated.
                Must be specified with `listen-stream`.
    * `listen-stream`: (optional) The full path of the stream socket or an
                abstract socket. When specifying an absolute path, it should
                normally be in one of the app-specific writable directories.
                When specifying an abstract socket, it must start with '@' and
                typically be followed by either the snap package name or the
                snap package name followed by '\_' and any other characters
                (eg, '@name' or '@name\_something').

* `slots`: a map of interfaces

## Interfaces

The `old-security` interface is used to make porting existing snaps easier.
It provides the following parameters:
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


## license.txt

A license text that the user must accept before the snap can be
installed.

## gui/ directory

The gui directory contains GUI releated files for the snap.

### dekstop files

The `gui/` directory may contain .desktop files for the snap. Those
desktop files may contain all valid desktop entries from the xdg
Desktop Entry Specification version 1.1 with some exceptions listed
below. If there is a line with an unknown key or an unofficial key
that line is silently removed from the desktop file on install.

Only `Exec=` lines that start with `Exec=$snap.$app` are valid, but
arguments may be passed. E.g. for a snap like:
```
name: http
version: 1.0
apps:
 GET:
  command: myhttpdownlaoder.get-stuff
```

With a desktop file:
```
[Desktop Entry]
Name=My downloader
Exec=http.GET %U
```

The `Exec=` line is valid because it starts with `Exec=http.GET` (the
snap is called "http" and the app is called "GET").


### Unsupported desktop keys

The `DBusActivatable`, `TryExec` and `Implements` keys are currently
not supported and will be silently removed from the desktop file on
install.

## hooks/ directory

See `config.md` for details.

# Examples

See https://github.com/ubuntu-core/snappy-testdata for up-to-date examples.
