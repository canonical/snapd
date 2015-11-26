# Snappy Ubuntu Core REST API

Version: 1.0.2 DRAFT

## Versioning

As the API evolves, some changes are deemed backwards-compatible (such
as adding methods or verbs, or adding members to the returned JSON
objects) and don't warrant an endpoint change; some changes won't be
backwards compatible, and will be exposed under a new endpoint.

## Connecting

While it is expected to allow clients to connect using HTTPS over a
TCP socket, at this point only a unix socket is supported. The socket
is `/run/snapd.socket`.

## Authentication

Authentication over the unix socket is delegated to UNIX ACLs. At this
point only root can connect to it; regular user access is not
implemented yet, but should be doable once `SO_PEERCRED` is supported
to determine privilege levels.

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

The HTTP code will be 200 (`OK`), or 201 (`Created`, in which case the
`Location` HTTP header will be set), as appropriate.

### Background operation

When a request results in a background operation, the HTTP code is set
to 202 (`Accepted`) and the `Location` HTTP header is set to the
operation's URL.

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

## /1.0
### GET

* Description: Server configuration and environment information
* Authorization: guest
* Operation: sync
* Return: Dict with the operating system's key values.

#### Sample result:

```javascript
{
 "default_channel": "edge",
 "flavor": "core",
 "api_compat": "1",           // increased on minor API changes
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
    "hello-world.canonical": {
      "description": "hello-world",
      "download_size": "22212",
      "icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
      "installed_size": "-1",          // always -1 if not installed
      "name": "hello-world",
      "origin": "canonical",
      "resource": "/1.0/packages/hello-world.canonical",
      "status": "not installed",
      "type": "app",
      "version": "1.0.18"
    },
    "http.chipaca": {
      "description": "HTTPie in a snap\nno description",
      "download_size": "1578272",
      "icon": "/1.0/icons/http.chipaca_3.1.png",
      "installed_size": "1821897",
      "name": "http",
      "origin": "chipaca",
      "resource": "/1.0/packages/http.chipaca",
      "status": "active",
      "type": "app",
      "version": "3.1"
    },
    "ubuntu-core.ubuntu": {
      "description": "A secure, minimal transactional OS for devices and containers.",
      "download_size": "19845748",
      "icon": "",               // core might not have an icon
      "installed_size": "-1",   // core doesn't have installed_size (yet)
      "name": "ubuntu-core",
      "origin": "ubuntu",
      "resource": "/1.0/packages/ubuntu-core.ubuntu",
      "status": "active",
      "type": "core",
      "update_available": "247",
      "version": "241"
    }
 }
}
```

#### Fields
* `packages`
    * `status`: can be either `not installed`, `installed`, `active` (i.e. is
      current), `removed` (but data present); there is no `purged` state, as a
      purged package is undistinguishable from a non-installed package.
    * `name`: the package name.
    * `version`: a string representing the version.
    * `icon`: a url to the package icon, possibly relative to this server.
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

It's also possible to provide the package as the entire body of a `POST` (not a
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
`leave_old`| `install` `update` `remove` | A boolean, default is false (do not leave old packages around). Equivalent to commandline's `--no-gc`.
`license`  | `install` `update` | A JSON object with `intro`, `license`, and `agreed` fields, the first two of which must match the license (see the section “A note on licenses”, below).

#### A note on licenses

When requesting to install a package that requires agreeing to a license before
install succeeds, or when requesting an update to a package with such an
agreement that has an updated license version, the initial request will fail
with an error, and the error object will contain the intro and license texts to
present to the user for their approval. An example of the command's `output`
field would be

```javascript
"output": {
    "obj": {
        "agreed": false,
        "intro": "licensed requires that you accept the following license before continuing",
        "license": "In order to use this software you must agree with us."
    },
    "str": "License agreement required."
}
```

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
 "output": {},
 "resource": "/1.0/packages/camlistore.sergiusens",
 "status": "running",                   // or “succeeded” or “failed”
 "updated_at": "1415639996451214"       // Last update timestamp
}
```

### DELETE

* Description: If the operation has completed, `DELETE` will remove the
  entry. Otherwise it is an error.
* Authorization: trusted
* Operation: sync
* Return: standard return value or standard error

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

## /1.0/capabilities
### GET

* Description: List of snappy capabilities
* Authorization: guest
* Operation: sync
* Return: information about all of the snappy capabilities 

The result is a JSON object with a capabilities key; its value itself is a JSON
object whose keys are capability names (e.g., "power-button") and whose values
describe that capability.

Each capability has the following attributes:

name:
	Name is a key that identifies the capability. It must be unique within its
	context, which may be either a snap or a snappy runtime.

label:
	Label provides an optional title for the capability to help a human tell
	which physical device this capability is referring to. It might say "Front
	USB", or "Green Serial Port", for example.

type:
	Type defines the type of this capability. The capability type defines the
	behavior allowed and expected from providers and consumers of that
	capability, and also which information should be exchanged by these
	parties.

attrs:
	Attrs are key-value pairs that provide type-specific capability details.
	The attribute 'attrs' itself may not be present if there are no attributes
	to mention.

Sample result:

```javascript
{
	"capabilities": {
		"power-button": {
			"resource": "/1.0/capabilities/power-button",
			"name": "power-button",
			"label": "Power Button",
			"type": "evdev",
			"attrs": {
				"path": "/dev/input/event2"
			},
		}
	}
}
```
