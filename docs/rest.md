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
 "flavor": "core",
 "series": "16",
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

## /v2/find
### GET

* Description: Find snaps in the store
* Access: authenticated
* Operation: sync
* Return: list of snaps in the store that match the search term and
  that this system can handle.

### Parameters:

#### `q`

Query.

#### `channel`

Which channel to search in.

#### Sample result:

[//]: # keep the fields sorted, both in the sample and its description below. Makes scanning easier

```javascript
[{
      "description": "This is a simple hello world example.",
      "developer": "canonical",
      "download-size": 20480,
      "icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
      "name": "hello-world",
      "resource": "/v2/snaps/hello-world",
      "revision": 25,
      "status": "available",
      "summary": "Hello world example",
      "type": "app",
      "version": "6.0",
      "prices": {"EUR": 1.99, "USD": 2.49}
    }, {
      "description": "no description",
      "developer": "chipaca",
      "download-size": 1110016,
      "icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/10/http.png",
      "name": "http",
      "resource": "/v2/snaps/http",
      "revision": 14,
      "status": "available",
      "summary": "HTTPie in a snap",
      "type": "app",
      "version": "4.6692016"
}]
```

##### Fields

[//]: # keep the fields sorted, both in the description and the sample above. Makes scanning easier

* `description`: snap description
* `download-size`: how big the download will be.
* `icon`: a url to the snap icon, possibly relative to this server.
* `name`: the snap name.
* `prices`: JSON object with properties named by ISO 4217 currency code. The values of the properties are numerics representing the cost in each currency. For free snaps, the "prices" property is omitted.
* `revision`: a number representing the revision.
* `status`: can be either `available`, or `priced` (i.e. needs to be bought to become available)
* `summary`: one-line summary
* `type`: the type of snap; one of `app`, `kernel`, `gadget`, or `os`.
* `version`: a string representing the version.

[//]: # seriously, keep the fields sorted!

#### Result meta data:

```javascript
{
 "suggested-currency": "GBP"
}
```

##### Fields

* `suggested-currency`: the suggested currency to use for presentation, 
   derived by Geo IP lookup.

## /v2/snaps
### GET

* Description: List of snaps
* Access: authenticated
* Operation: sync
* Return: list of snaps installed in this Ubuntu Core system, as for `/v2/find`

Sample result:

[//]: # keep the fields sorted, both in the description and the sample above. Makes scanning easier

```javascript
[{
      "summary": "HTTPie in a snap",
      "description": "no description",
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
      "icon": "",                  // core might not have an icon
      "installed-size": 67784704,
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
}]
```

#### Fields

In addition to the fields described in `/v2/find`:

[//]: # keep the fields sorted!

* `channel`: which channel the package is currently tracking.
* `installed-size`: how much space the snap itself (not its data) uses.
* `install-date`: the date and time when the snap was installed.
* `status`: can be either `installed` or `active` (i.e. is current).

furthermore, `price` cannot occur in the output of `/v2/snaps`.

### POST

* Description: Install an uploaded snap to the system.
* Access: trusted
* Operation: async
* Return: background operation or standard error

#### Input

The snap to install must be provided as part of the body of a
`mutlipart/form-data` request. The form should have one file
named "snap".

## /v2/snaps/[name]
### GET

* Description: Details for an installed snap
* Access: authenticated
* Operation: sync
* Return: snap details (as in `/v2/snaps`)

### POST

* Description: Install, refresh, or remove
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
`action`   |                   | Required; a string, one of `install`, `refresh`, or `remove`
`channel`  | `install` `update` | From which channel to pull the new package (and track henceforth). Channels are a means to discern the maturity of a package or the software it contains, although the exact meaning is left to the application developer. One of `edge`, `beta`, `candidate`, and `stable` which is the default.

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
