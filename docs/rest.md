# Snappy Ubuntu Core REST API

Version: 1.0.1 DRAFT (look for "not implemented" to find bits still in flux or
not implemented)

## Versioning

As the API evolves, some changes are deemed backwards-compatible (such
as adding methods or verbs, or adding members to the returned JSON
objects) and don’t warrant an endpoint change; some changes won’t be,
and will be exposed under a new endpoint.

## Connecting

While it is expected to allow clients to connect using HTTPS over a
TCP socket, as described above, at this point only a unix socket is
supported. The socket is `/run/snapd`.

## Authentication

Authentication over the unix socket is delegated to UNIX ACLs. At this point
only root can connect to it; later, regular user access is not implemented
yet, but should be able to once SO_PEERCRED is supported to determine
privilege levels.

## Responses

All responses are `application/json` unless noted otherwise. There are
three standard return types:

* Standard return value
* Background operation
* Error

Status codes follow that of HTTP.

### Standard return value

For a standard synchronous operation, the following JSON object is
returned:

```javascript
{
 "result": {},               // Extra resource/action specific data
 "status": "OK",
 "status_code": 200,
 "type": "sync"
}
```

The HTTP code must be 200.

### Background operation

When a request results in a background operation, the HTTP code is set
to 202 (Accepted) and the Location HTTP header is set to the operation
URL.

The body is a json object with the following structure:

```javascript
{
 "result": {
   "resource": "/1.0/operations/[uuid]",     // see below
   "status": "running",
   "created_at": "..."                       // and other operation fields
 },
 "status": "Accepted",
 "status_code": 202,
 "type": "async"
}
```

The response body is mostly provided as a user friendly way of seeing
what's going on without having to pull the target operation; all
information in the body can also be retrieved from the background
operation URL.

### Error

There are various situations in which something may immediately go
wrong, in those cases, the following return value is used:

```javascript
{
 "result": {},            // may contain more details, for debugging
 "status": "Bad Request", // text description of status_code
 "status_code": 400,      // or 401, etc. (same as HTTP code)
 "type": "error"
}
```

HTTP code must be one of of 400, 401, 403, 404, 409, 412 or 500.

### Timestamps

Timestamps are presented in µs since the epoch UTC, formatted as a decimal
string. For example, `"1234567891234567"` represents
`2009-02-13T23:31:31.234567`.

## /
### GET

* Description: List of supported APIs
* Authorization: guest
* Operation: sync
* Return: list of supported API endpoint URLs (by default `["/1.0"]`)


## /1.0
### GET

* Description: Server configuration and environment information
* Authorization: guest
* Operation: sync
* Return: Dict with the operating system’s key values.

#### Sample result:

```javascript
{
 "default_channel": "edge",
 "flavor": "core",
 "api_compat": "0",           // increased on minor API changes
 "release": "15.04",
 "store": "store-id"          // only if not default
}
```

## /1.0/packages
### GET

* Description: List of packages
* Authorization: trusted
* Operation: sync
* Return: list of URLs for packages this Ubuntu Core system can handle.

The result is a JSON object with a packages key; its value is itself a
JSON object whose keys are qualified package names (e.g.,
hello-world.canonical), and whose values describe that package.

Sample result:

```javascript
{
 "packages": {
   "dd.canonical": {
     "description": "A description",
     "download_size": "23456",
     "icon": "/icons/dd.png",
     "installed_size": "-1",          // always -1 if not installed
     "name": "dd",
     "origin": "canonical",
     "resource": "/1.0/packages/dd.canonical",
     "status": "not installed",
     "type": "app",
     "vendor": "Somebody",
     "version": "0.1"
   },
   "pastebinit.mvo": {
     "description": "A description",
     "download_size": "23456",
     "icon": "http://storeurl/icon.png",
     "name": "pastebinit.mvo",
     "installed_size": "-1",
     "operation": {
       "created_at": 1415639996,
       "may_cancel": true,
       "resource": "/1.0/packages/pastebinit.mvo",
       "status": "Running",
       "status_code": 100,
       "updated_at": 1415639996
     },
     "origin": "mvo",
     "resource": "/1.0/packages/pastebinit.mvo",
     "rollback_available": "0.9",
     "status": "installing",
     "type": "app",
     "update_available": "1.1",
     "vendor": "Michael Vogt",
     "version": "0.8"
   },
   "ubuntu-core.canonical": {
     "description": "A description",
     "download_size": "23456",
     "icon": "",               // core might not have an icon
     "installed_size": "-1",   // core doesn’t have installed_size (yet)
     "name": "ubuntu-core",
     "origin": "canonical",
     "resource": "/1.0/packages/ubuntu-core.canonical",
     "status": "active",
     "type": "core",
     "vendor": "Canonical",
     "version": "43"
   }
 }
}
```

#### Fields
* `packages`
    * `status`: can be either `not installed`, `installing`, `installed`,
      `uninstalling`, `active` (i.e. is current), `removed` (but data present),
      `purging`. For statuses that signal a background operation is in course,
      see the `operation` field. Transient states not implemented yet.
    * `name`: the package name.
    * `version`: a string representing the version.
    * `vendor`: a string representing the vendor.
    * `icon`: a url to the package icon, possible relative to this server.
    * `type`: the type of snappy package; one of `app`, `framework`, `kernel`,
      `gadget`, or `os`.
    * `description`: package description
    * `installed_size`: for installed packages, how much space the package
      itself (not its data) uses, formatted as a decimal string.
    * `download_size`: for not-installed packages, how big the download will
      be, formatted as a decimal string.
    * `operation`: if the state signals that an operation is underway
      (e.g. installing), the operation field describes that operation
    * `rollback_available`: if present and not empty, it means the package can
      be rolled back to the version specified as a value to this entry.
    * `update_available`: if present and not empty, it means the package can be
      updated to the version specified as a value to this entry.

### POST

* Description: Sideload a package to the system. 
* Authorization: trusted
* Operation: async
* Return: background operation or standard error

#### Input

The package to sideload should be provided as part of the body of a
`mutlipart/form-data` request. The form should have only one file. If it also
has an `allow-unsigned` field (with any value), the package may be unsigned;
otherwise attempting to sideload an unsigned package will result in a failed
background operation.

It’s also possible to provide the package as the entire body of a `POST` (not a
multipart request). In this case the header `X-Allow-Unsigned` may be used to
allow sideloading unsigned packages.

### PUT

* Description: change configuration for active packages. It is an error to
  attempt to change configuration for non-active packages; if a configuration
  change is requested for a package that is not active in the system the whole
  command is aborted even if other packages that are active are specified in
  the same command.
* Authorization: trusted
* Operation: sync
* Return: configuration for all listed packages

The request body is expected to be a JSON object with keys being the qualified
package name(s) to configure, and the values will be passed to the configure
hooks of the packages. The background operation information will similarly
list individual statuses of the configuration changes.

#### Sample input:

```javascript
{
   "dd.canonical": "some-option: 42",
   "hello-world.canonical": "greeting: Hi"
}
```

#### Sample result:

```javascript
{
   "dd.canonical": "some-option: 42\nsome-other-option: true",
   "hello-world.canonical": "greeting: Hi\nheader: false"
}
```

## /1.0/packages/[name]
### GET

* Description: Details for a  package
* Authorization: trusted
* Operation: sync
* Return: package details (as in `/1.0/packages/`)


### POST

* Description: Install, update, remove, purge, activate, deactivate, or
  rollback the package
* Authorization: trusted
* Operation: async
* Return: background operation or standard error

#### Sample input

```javascript
{
 "action": "install"
}
```

#### Fields in the input object

field      | ignored except in action | description
-----------|-------------------|------------
`action`   |                   | Required; a string, one of `install`, `update`, `remove`, `purge`, `activate`, `deactivate`, or `rollback`.
`leave_old`| `install` `update` `remove` | A boolean, default is false (do not leave old packages around). Equivalent to commandline’s `--no-gc`.
`config`   | `install` | An object, passed to config after installation. See `.../config`. If missing, config isn’t called.

#### A note on licenses

The output field in the operation object for installation may specify that an
additional step is needed, to confirm the user accepts the license specified
in the package that’s installing. Like so:

```javascript
{
 "resource": "/1.0/operations/xyzzy",
// ... other operation fields ...,
 "output": {
   "license_ack_needed": true,
   "license_text": "Long license text\n\nMay include newlines etc.",
 }
}
```

The client must then present this license text to the user and ask for their
acceptance. The operation will not complete until the client accepts (or
declines) the license, by POSTing the appropriate response to the
operation. If the server is restarted and the operation is lost, and the
client redoes the installation, they may. See the section on POSTing to
.../operations/ for more details.

## /1.0/packages/[name]/services

Query an active package for information about its services, and alter the
state of those services. Commands under `.../services` will return an error if
the package is not active.

### GET

* Description: Services for a package
* Authorization: trusted
* Operation: sync
* Return: service configuration

Returns a JSON object with a result key, its value is a list of JSON objects
where the package name is the item key. The value is another JSON object that
has three keys [`op`, `spec`, `status`], spec and status are JSON objects that
provide description about the service as well as its systemd unit.

#### Sample result:

```javascript
{
  "result": {
    "xkcd-webserver": {
      "op": "status",
      "spec": {
        "name": "xkcd-webserver",
        "description": "A fun webserver",
        "start": "bin/xkcd-webserver",
        "stop-timeout": "30s",
        "caps": [
          "networking",
          "network-service"
        ]
      },
      "status": {
        "service_file_name": "xkcd-webserver_xkcd-webserver_0.5.service",
        "load_state": "loaded",
        "active_state": "inactive",
        "sub_state": "dead",
        "unit_file_state": "enabled",
        "package_name": "xkcd-webserver",
        "service_name": "xkcd-webserver"
      }
    }
  },
  "status": "OK",
  "status_code": 200,
  "type": "sync"
}
```

### PUT

* Description: Put all services of a package into a specific state
* Authorization: trusted
* Operation: async

#### Sample input:

```javascript
{
"action": "start|stop|restart|enable|disable"
}
```

## /1.0/packages/[name]/services/[name]

### GET

* Description: Service for a package
* Authorization: trusted
* Operation: sync
* Return: service configuration

The result is a JSON object with a `result` key where the value is a JSON object
that includes a single object from the list of the upper level endpoint
(`/1.0/packages/[name]/services`).

#### Sample result:

```javascript
{
  "result": {
    "op": "status",
    "spec": {
      "name": "xkcd-webserver",
      "description": "A fun webserver",
      "start": "bin/xkcd-webserver",
      "stop-timeout": "30s",
      "caps": [
        "networking",
        "network-service"
      ]
    },
    "status": {
      "service_file_name": "xkcd-webserver_xkcd-webserver_0.5.service",
      "load_state": "loaded",
      "active_state": "inactive",
      "sub_state": "dead",
      "unit_file_state": "enabled",
      "package_name": "xkcd-webserver",
      "service_name": "xkcd-webserver"
    }
  },
  "status": "OK",
  "status_code": 200,
  "type": "sync"
}
```

### PUT

* Description: Put the service into a specific state
* Authorization: trusted
* Operation: async

#### Sample input:

```javascript
{
"action": "start|stop|restart|enable|disable"
}
```

## /1.0/packages/[name]/services/[name]/logs

### GET

* Description: Logs for the service from a package
* Authorization: trusted
* Operation: sync
* Return: service logs

#### Sample result:

```javascript
[
   {
       "timestamp": "1440679470679901",
       "message": "something happened",
       "raw": {}
   },
   {
       "timestamp": "1440679470680968",
       "message": "bla bla",
       "raw": {}
    }
]
```

## /1.0/packages/[name]/config

Query an active package for information about its configuration, and alter
that configuration. Will return an error if the package is not active.

### GET

* Description: Configuration for a package
* Authorization: trusted
* Operation: sync
* Return: package configuration

#### Sample result:

```javascript
"config:\n  ubuntu-core:\n    autopilot: false\n    timezone: Europe/Berlin\n    hostname: localhost.localdomain\n"
```

Notes: user facing implementations in text form must show this data using yaml.

### PUT

* Description: Set configuration for a package
* Authorization: trusted
* Operation: sync
* Return: package configuration

#### Sample input:

```javascript
        config:\n  ubuntu-core:\n    autopilot: true\n
```

#### Sample result:

```javascript
"config:\n  ubuntu-core:\n    autopilot: true\n    timezone: Europe/Berlin\n    hostname: localhost.localdomain\n"
```

## /1.0/events

Not implemented yet; poll on .../operations/ until this is final.

This URL isn't a real REST API endpoint, instead doing a GET query on it will
upgrade the connection to a websocket on which notifications will be sent.

### GET

* Description: websocket upgrade
* Authorization: trusted
* Operation: sync
* Return: none (never ending flow of events)

#### Supported arguments
* type: comma separated list of notifications to subscribe to (defaults to “operations,logging”)

#### Notification types
* operations
* logging

This never returns. Each notification is sent as a separate JSON dict:

```javascript
{
   'timestamp': "1415639996457234",        # Current timestamp
   'type': "operations",                   # Notification type
   'operation': {...}                      # Operation object
}

{
   'timestamp': "141563999675243",
   'type': "logging",
   'log': {'message': "Package [name] installed"}
}
```

## /1.0/operations/<uuid>

### GET

* Description: background operation
* Authorization: trusted
* Operation: sync
* Return: dict representing a background operation

#### Sample result:

```javascript
{
 "created_at": "1415639996123456",      // Creation timestamp
 "may_cancel": true,
 "output": {},
 "resource": "/1.0/packages/camlistore.sergiusens",
 "status": "running",                   // or “succeeded” or “failed”
 "updated_at": "1415639996451214"       // Last update timestamp
}
```

### DELETE

All background operations will have `may_cancel` set to false.

* Description: cancel an operation if running. If the operation has
  `may_cancel` set to `true` calling this will set the state to “Cancelling”
  rather than actually removing the entry. If `may_cancel` is `false`, it is an
  error (will result in “Bad Method”) unless the operation has completed; if
  the operation has completed, `DELETE` will remove the entry.
* Authorization: trusted
* Operation: sync
* Return: standard return value (Accepted status) or standard error

### POST

Used to interact with a background operation. Request body must be a JSON
object, with members depending on the interaction as in the table
below. Response is the operation response as for GET above.

interaction | fields | values
------------|--------|--------
license agreement | `accepted` | boolean, defaults to `false`

## /1.0/icons/[icon]

### GET

Gets a locally-installed snap's icon. The response will be the raw contents of
the icon file; the content-type will be set accordingly.

This fetches the icon that was downloaded from the store at install time.

## /1.0/icons/[name]/icon

### GET

Get an icon from a snap installed on the system. The response will be the raw
contents of the icon file; the content-type will be set accordingly.

This fetches the icon from the package itself.
