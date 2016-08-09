# Snappy Ubuntu Core REST API

Version: v2pre0

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
`two-factor-required` | the client needs to retry the `login` command including an OTP
`two-factor-failed` | the OTP provided wasn't recognised
`login-required` | the requested operation cannot be performed without an authenticated user. This is the kind of any other 401 Unauthorized response.
`invalid-auth-data` | the authentication data provided failed to validate (e.g. a malformed email address). The `value` of the error is an object with a key per failed field and a list of the failures on each field.

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
 "series": "16",
 "version": "2.0.17",
 "os-release": {
   "id": "ubuntu",
   "version-id": "17.04",
 },
 "on-classic": true,
 "store": "store-id"          // only if not default
}
```

## `/v2/login`
### `POST`

* Description: Log user in the store
* Access: trusted
* Operation: sync
* Return: Dict with the authenticated user information.

#### Sample input

```javascript
{
  "username": "foo@bar.com", // username is an email
  "password": "swordfish",   // the password (!)
  "otp": "123456"            // OTP, if the account needs it
}
```

#### Sample result:

```javascript
{
 "macaroon": "serialized-store-macaroon",
 "discharges": ["discharge-for-macaroon-authentication"]
}
```

See also the error kinds `two-factor-required` and
`two-factor-failed`.


## /v2/find
### GET

* Description: Find snaps in the store
* Access: authenticated
* Operation: sync
* Return: list of snaps in the store that match the search term and
  that this system can handle.

### Parameters:

#### `q`

Search for snaps that match the given string. This is a weighted broad
search, meant as the main interface to searching for snaps.

#### `name`

Search for snaps whose name matches the given string. Can't be used
together with `q`. This is meant for things like autocompletion. The
match is exact (i.e. find would return 0 or 1 results) unless the
string ends in `*`.

#### `select`

Alter the collection searched:

* `refresh`: search refreshable snaps. Can't be used with `q`, nor `name`.
* `private`: search private snaps (by default, find only searches
  public snaps). Can't be used with `name`, only `q` (for now at
  least).

#### `private`

A boolean flag that, if `true` (or `t` or `yes` or...), makes the search look
in the user's private snaps. Requires that the user be authenticated. Only
works with broad (`text`-prefix) search; defaults the prefix to `text`.

#### Sample result:

[//]: # (keep the fields sorted, both in the sample and its description below. Makes scanning easier)

```javascript
[{
      "channel": "stable",
      "confinement": "strict",
      "description": "Moon-buggy is a simple character graphics game, where you drive some kind of car across the moon's surface.  Unfortunately there are dangerous craters there.  Fortunately your car can jump over them!\r\n",
      "developer": "dholbach",
      "download-size": 90112,
      "icon": "",
      "id": "2kkitQurgOkL3foImG4wDwn9CIANuHlt",
      "name": "moon-buggy",
      "private": false,
      "resource": "/v2/snaps/moon-buggy",
      "revision": "11",
      "status": "available",
      "summary": "Drive a car across the moon",
      "type": "app",
      "version": "1.0.51.11"
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

[//]: # (keep the fields sorted, both in the description and the sample above. Makes scanning easier)

* `channel`: which channel the snap is currently tracking.
* `confinement`: the confinement requested by the snap itself; one of `strict` or `devmode`.
* `description`: snap description.
* `developer`: developer who created the snap.
* `download-size`: how big the download will be.
* `icon`: a url to the snap icon, possibly relative to this server.
* `id`: unique ID for this snap.
* `name`: the snap name.
* `prices`: JSON object with properties named by ISO 4217 currency code. The values of the properties are numerics representing the cost in each currency. For free snaps, the "prices" property is omitted.
* `private`: true if this snap is only available to its author.
* `resource`: HTTP resource for this snap.
* `revision`: a number representing the revision.
* `status`: can be either `available`, or `priced` (i.e. needs to be bought to become available).
* `summary`: one-line summary.
* `type`: the type of snap; one of `app`, `kernel`, `gadget`, or `os`.
* `version`: a string representing the version.

[//]: # (seriously, keep the fields sorted!)

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

[//]: # (keep the fields sorted, both in the description and the sample above. Makes scanning easier)

```javascript
[{
      "apps": [{"name": "moon-buggy"}]
      "channel": "stable"
      "confinement": "strict"
      "description": "Moon-buggy is a simple character graphics game, where you drive some kind of car across the moon's surface.  Unfortunately there are dangerous craters there.  Fortunately your car can jump over them!\r\n",
      "developer": "dholbach",
      "devmode": false,
      "icon": "",
      "id": "2kkitQurgOkL3foImG4wDwn9CIANuHlt",
      "install-date": "2016-05-17T09:36:53+12:00",
      "installed-size": 90112,
      "name": "moon-buggy",
      "private": false,
      "resource": "/v2/snaps/moon-buggy",
      "revision": "11",
      "status": "active",
      "summary": "Drive a car across the moon",
      "trymode": false,
      "type": "app",
      "version": "1.0.51.11"
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

[//]: # (keep the fields sorted!)

* `apps`: JSON array of apps the snap provides. Each app has a `name` field to name a binary this app provides.
* `devmode`: true if the snap is currently installed in development mode.
* `installed-size`: how much space the snap itself (not its data) uses.
* `install-date`: the date and time when the snap was installed.
* `status`: can be either `installed` or `active` (i.e. is current).
* `trymode`: true if the app was installed in try mode.

furthermore, `download-size` and `price` cannot occur in the output of `/v2/snaps`.

### POST

* Description: Install an uploaded snap to the system.
* Access: trusted
* Operation: async
* Return: background operation or standard error

#### Input

The snap to install must be provided as part of the body of a
`multipart/form-data` request. The form should have one file
named "snap".

## /v2/snaps/[name]
### GET

* Description: Details for an installed snap
* Access: authenticated
* Operation: sync
* Return: snap details (as in `/v2/snaps`)

### POST

* Description: Install, refresh, revert or remove
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
`action`   |                   | Required; a string, one of `install`, `refresh`, `remove`, `revert`, `enable`, or `disable`.
`channel`  | `install` `update` | From which channel to pull the new package (and track henceforth). Channels are a means to discern the maturity of a package or the software it contains, although the exact meaning is left to the application developer. One of `edge`, `beta`, `candidate`, and `stable` which is the default.

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
    "slots": [{"snap": "canonical-pi2",   "slot": "pin-13"}],
    "plugs": [{"snap": "keyboard-lights", "plug": "capslock-led"}]
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

## /v2/buy

### POST

* Description: Buy the specified snap
* Access: authenticated
* Operation: sync
* Return: Dict with buy state.

#### Sample input using default payment method:

```javascript
{
    "snap-id": "2kkitQurgOkL3foImG4wDwn9CIANuHlt",
    "snap-name": "moon-buggy",
    "price": "2.99",
    "currency": "USD"
}
```

#### Sample input specifying specific credit card:

```javascript
{
    "snap-id": "2kkitQurgOkL3foImG4wDwn9CIANuHlt",
    "snap-name": "moon-buggy",
    "price": "2.99",
    "currency": "USD",
    "backend-id": "credit_card",
    "method-id": 1
}
```

#### Sample result:

```javascript
{
 "state": "Complete",
}
```

## /v2/buy/methods

### GET

* Description: Get a list of the available payment methods
* Access: authenticated
* Operation: sync
* Return: Dict with payment methods.

#### Sample result with one method that allows automatic payment:

```javascript
{
    "allows-automatic-payment": true,
    "methods": [
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 1111 (exp 23/2020)",
        "id": 123,
        "preferred": true,
        "requires-interaction": false
      }
    ]
  }
```

#### Sample with 3 methods and no automatic payments:

```javascript
{
    "allows-automatic-payment": false,
    "methods": [
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 1111 (exp 23/2020)",
        "id": 123,
        "preferred": false,
        "requires-interaction": false
      },
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 2222 (exp 23/2025)",
        "id": 234,
        "preferred": false,
        "requires-interaction": false
      },
      {
        "backend-id": "rest_paypal",
        "currencies": ["USD", "GBP", "EUR"],
        "description": "PayPal",
        "id": 345,
        "preferred": false,
        "requires-interaction": true
      }
    ]
  }
```
