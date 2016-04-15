# Snappy Ubuntu Core REST API

Version: v2pre0

Note: The v2 API is going to be very different from the 1.0; right now, not
so much.

## Versioning

As the API evolves, some changes are deemed backwards-compatible (such
as adding methods or verbs, or adding members to the returned JSON
objects) and don't warrant an endpoint change; some changes won't be
backwards compatible, and will be exposed under a new endpoint.

## Connecting

While it is expected to allow clients to connect using HTTPS over a
TCP socket, at this point only a UNIX socket is supported. The socket
is `/run/snapd.socket`.

## Authentication

The API documents three levels of access: *guest*, *authenticated* and
*trusted*. The trusted level is allowed to modify all aspects of the
system, the authenticated level can query most but not all aspects,
and the guest level can only query static system-level information.

Authentication over the unix socket is delegated to UNIX ACLs, and
uses `SO_PEERCRED` to determine privilege levels. In essence this
means that a user will be either *authenticated* or *trusted*, with
the latter restricted to the superuser.

[//]: # (QUESTION: map system user nobody to guest?)

## Responses

All responses are `application/json` unless noted otherwise. There are
three standard return types:

* Standard return value
* Background operation
* Error

Status codes follow that of HTTP. Standard and background operation responses
are capable of returning additional meta data key/values as part of the returned
JSON object.

### Standard return value

For a standard synchronous operation, the following JSON object is
returned:

```javascript
{
 "result": {},               // Extra resource/action specific data
 "status": "OK",
 "status-code": 200,
 "type": "sync"
}
```

The HTTP code will be 200 (`OK`), or 201 (`Created`, in which case the
`Location` HTTP header will be set), as appropriate.

### Background operation

When a request results in a background operation, the HTTP code is set
to 202 (`Accepted`) and the `Location` HTTP header is set to the
operation's URL.

The body is a JSON object with the following structure:

```javascript
{
 "result": {
     ...
 },
 "status": "Accepted",
 "status-code": 202,
 "type": "async"
 "change": "adWf",
}
```

Information about the background operation progress can be retrieved
from the referenced change.

### Error

There are various situations in which something may immediately go
wrong, in those cases, the following return value is used:

```javascript
{
 "result": {
   "message": "human-friendly description of the cause of the error",
   "kind": "store-error",  // one of a list of kinds (TBD), only present iff "value" is present
   "value": {"...": "..."} // kind-specific object, as required
 },
 "status": "Bad Request", // text description of status-code
 "status-code": 400,      // or 401, etc. (same as HTTP code)
 "type": "error"
}
```

HTTP code must be one of of 400, 401, 403, 404, 405, 409, 412 or 500.

Error *results* will also be used in the output of `async` responses.

If, in implementing a client, you find yourself keying off of
`message` to alter the behaviour of your client to e.g. better inform
the user of the error or otherwise adapt to the error condition,
**STOP** and *talk to us*; this is where `kind` comes in. New entries
for `kind` (and associated `value` metadata) will be added as needed
by client implementations.

#### Error kinds

kind               | value description
-------------------|--------------------
`license-required` | see "A note on licenses", below

### Timestamps

Timestamps are presented in RFC3339 format, with µs precision, and in
UTC. For example, `2009-02-13T23:31:31.234567Z`.

## `/`

Reserved for human-readable content describing the service.

## `/v2/system-info`
### `GET`

* Description: Server configuration and environment information
* Access: guest
* Operation: sync
* Return: Dict with the operating system's key values.

#### Sample result:

```javascript
{
 "default-channel": "edge",
 "flavor": "core",
 "api-compat": "1",           // increased on minor API changes
 "release": "15.04",
 "store": "store-id"          // only if not default
}
```

## `/v2/login`
### `POST`

* Description: Log user in the store
* Access: trusted
* Operation: sync
* Return: Dict with the authenticated user information.

#### Sample result:

```javascript
{
 "macaroon": "serialized-store-macaroon",
 "discharges": ["discharge-for-macaroon-authentication"]
}
```

## /v2/snaps
### GET

* Description: List of snaps
* Access: authenticated
* Operation: sync
* Return: list of snaps this Ubuntu Core system can handle.

Sample result:

```javascript
[{
      "summary": "Hello world example",
      "description": "This is a simple hello world example.",
      "download-size": 22212,
      "icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
      "installed-size": -1,          // always -1 if neither "active" nor "installed"
      "name": "hello-world",
      "developer": "canonical",
      "resource": "/v2/snaps/hello-world",
      "status": "available",
      "type": "app",
      "revision": 17,
      "version": "1.0.18",
      "channel": "stable"
    }, {
      "summary": "HTTPie in a snap",
      "description": "no description",
      "download-size": 1578272,
      "icon": "/v2/icons/http/icon",
      "installed-size": 1821897,
      "install-date": "2016-03-10T13:16:52Z",
      "name": "http",
      "developer": "chipaca",
      "resource": "/v2/snaps/http",
      "status": "active",
      "type": "app",
      "version": "3.1",
      "revision": 1834,
      "channel": "stable"
    }, {
      "summary": "The ubuntu-core OS snap",
      "description": "A secure, minimal transactional OS for devices and containers.",
      "download-size": 19845748,
      "icon": "",               // core might not have an icon
      "installed-size": -1,     // core doesn't have installed-size (yet)
      "install-date": "2016-03-08T11:29:21Z",
      "name": "ubuntu-core",
      "developer": "canonical",
      "resource": "/v2/snaps/ubuntu-core",
      "status": "active",
      "type": "os",
      "update-available": 247,
      "version": "241",
      "revision": 99,
      "channel": "stable",
      "prices": {"EUR": 1.99, "USD": 2.49}
}]
```

#### Fields

* `status`: can be either `available`, `installed`, `active` (i.e. is
  current).
* `name`: the snap name.
* `version`: a string representing the version.
* `revision`: a number representing the revision.
* `icon`: a url to the snap icon, possibly relative to this server.
* `type`: the type of snap; one of `app`, `core`, `kernel`,
  `gadget`, or `os`.
* `description`: snap description
* `summary`: one-line summary
* `installed-size`: for installed snaps, how much space the snap
  itself (not its data) uses.
* `download-size`: for not-installed snaps, how big the download will
  be, formatted as a decimal string.
* `rollback-available`: if present and not empty, it means the snap can
  be rolled back to the revision specified as a value to this entry.
* `update-available`: if present and not empty, it means the snap can be
  updated to the revision specified as a value to this entry.
* `channel`: which channel the package is currently tracking.
* `prices`: JSON object with properties named by ISO 4217 currency code.
  The values of the properties are numerics representing the cost in each
  currency. For free snaps, the "prices" property is omitted.


Sample additional meta data:

```javascript
{
 "paging": {
    "total": 3,
    "page": 0,
    "pages": 1
  },
  "sources": [
    "local",
    "store"
  ]
}
```

#### Fields

* `paging`
    * `total`: the total number of snaps
    * `page`: the page number, starting from `0`
    * `pages`: the (approximate) number of pages
* `sources`
    a list of the sources that were queried (see the `sources` parameter, below)
* `suggested-currency`: the suggested currency to use for presentation, 
   derived by Geo IP lookup.

### Parameters [fixme: is that the right word for these?]

#### `sources`

Can be set to either `local` (to only list local snaps) or `store` (to
only list snaps from the store), or a comma-separated
combination. Defaults to `local,store`.

Note that excluding sources will result in incomplete (and in some
cases incorrect) information about installed packages: information
about updates will be absent if `store` is not included, whereas if
`local` is not included information about rollbacks will be missing,
and the package state for installed packages will be incorrect.

#### `types`

Restricts returned snaps to those with types included in the specified
comma-separated list. See the description of the `type` field of `snaps` in the
above section for possible values.

#### `page`

Request the given page when the server is paginating the
result. Defaults to `0`.

#### `q`

If present, only list snaps that match the query.

### POST

* Description: Sideload a snap to the system.
* Access: trusted
* Operation: async
* Return: background operation or standard error

#### Input

The snap to sideload should be provided as part of the body of a
`mutlipart/form-data` request. The form should have only one file. If it also
has an `allow-unsigned` field (with any value), the snap may be unsigned;
otherwise attempting to sideload an unsigned snap will result in a failed
background operation.

It's also possible to provide the snap as the entire body of a `POST` (not a
multipart request). In this case the header `X-Allow-Unsigned` may be used to
allow sideloading unsigned snaps.

## /v2/snaps/[name]
### GET

* Description: Details for a snap
* Access: authenticated
* Operation: sync
* Return: snap details (as in `/v2/snaps`)

### Parameters

#### `sources`

See `sources` for `/v2/snaps`.

### POST

* Description: Install, update, remove, activate, deactivate, or
  rollback the snap
* Access: trusted
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
`action`   |                   | Required; a string, one of `install`, `update`, `remove`, `activate`, `deactivate`, or `rollback`.
`channel`  | `install` `update` | From which channel to pull the new package (and track henceforth). Channels are a means to discern the maturity of a package or the software it contains, although the exact meaning is left to the application developer. One of `edge`, `beta`, `candidate`, and `stable` which is the default.
`leave-old`| `install` `update` `remove` | A boolean, equivalent to commandline's `--no-gc`. Default is false (do not leave old snaps around).
`license`  | `install` `update` | A JSON object with `intro`, `license`, and `agreed` fields, the first two of which must match the license (see the section "A note on licenses", below).

#### A note on licenses

When requesting to install a snap that requires agreeing to a license before
install succeeds, or when requesting an update to a snap with such an
agreement that has an updated license version, the initial request will fail
with an error, and the error object will contain the intro and license texts to
present to the user for their approval. An example of the command's `output`
field would be

```javascript
"output": {
    "value": {
        "agreed": false,
        "intro": "licensed requires that you accept the following license before continuing",
        "license": "In order to use this software you must agree with us."
    },
    "kind": "license-required",
    "message": "License agreement required."
}
```

## /v2/icons/[name]/icon

### GET

* Description: Get an icon from a snap installed on the system. The
  response will be the raw contents of the icon file; the content-type
  will be set accordingly and the Content-Disposition header will specify
  the filename.

  This fetches the icon from the snap itself.
* Access: guest

This is *not* a standard return type.

## /v2/assertions

### POST

* Description: Tries to add an assertion to the system assertion database.
* Authorization: trusted
* Operation: sync

The body of the request provides the assertion to add. The assertion
may also be a newer revision of a preexisting assertion that it will replace.

To succeed the assertion must be valid, its signature verified with a
known public key and the assertion consistent with and its
prerequisite in the database.

## /v2/assertions/[assertionType]
### GET

* Description: Get all the assertions in the system assertion database of the given type matching the header filters passed as query parameters
* Access: authenticated
* Operation: sync
* Return: stream of assertions

The response is a stream of assertions separated by double newlines.
The X-Ubuntu-Assertions-Count header is set to the number of
returned assertions, 0 or more.

## /v2/interfaces

### GET

* Description: Get all the plugs, slots and their connections.
* Access: authenticated
* Operation: sync
* Return: an object with two arrays of plugs, slots and their connections.

Sample result:

```javascript
{
    "slots": [
        {
            "snap":  "canonical-pi2",
            "slot":  "pin-13",
            "interface":  "bool-file",
            "label": "Pin 13",
            "connections": [
                {"snap": "keyboard-lights", "plug": "capslock-led"}
            ]
        }
    ],
    "plugs": [
        {
            "snap":  "keyboard-lights",
            "plug":  "capslock-led",
            "interface": "bool-file",
            "label": "Capslock indicator LED",
            "connections": [
                {"snap": "canonical-pi2", "slot": "pin-13"}
            ]
        }
    ]
}
```

### POST

* Description: Issue an action to the interface system
* Access: authenticated
* Operation: async
* Return: background operation or standard error

Available actions are:

- connect: connect the plug to the given slot.
- disconnect: disconnect the given plug from the given slot.

Sample input:

```javascript
{
    "action": "connect",
    "slots": {{"snap": "canonical-pi2",   "slot": "pin-13"}},
    "plugs": {{"snap": "keyboard-lights", "plug": "capslock-led"}}
}
```

## /v2/events

### GET

* Description: Websocket upgrade
* Access: trusted
* Operation: sync
* Return: nothing (never ending flow of events)

### Parameters

The default is for all notifications to be received but the following filters
are supported:

#### types

Comma separated list of notification types, either `logging` or `operations`.

#### resource

Generally the UUID of a background operation you are interested in.
