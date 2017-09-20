=========
 snapctl
=========

-------------------------------------
tool for accessing snap configuration
-------------------------------------

:Author: pawel.stolowski@canonical.com
:Date:   2017-09-19
:Copyright: Canonical Ltd.
:Version: 2.27
:Manual section: 1
:Manual group: snappy

SYNOPSIS
========

  snapctl COMMAND ARGUMENTS...

DESCRIPTION
===========

The `snapctl` command can be called by snap hooks (such as `configure`,
`install` or `remove`) or by applications to get or set configuration values of
the containing snap. The `snapctl` command cannot be used outside of these
contexts.

The configuration modified by `snapctl` is persistent and is only removed from
the disk when the last revision of the snap gets removed.

OPTIONS
=======

`get <key>`           Read configuration value assigned to <key>.

`set <key>=<value>`   Set configuration <value> for <key>.

`Get` flags 
-----------
-d                    Always return document, even with single key.
-t                    Strict typing with nulls and quoted strings.


EXIT STATUS
===========

`snapctl` exits with non-zero status if used outside of allowed contexts
(outside of hooks or apps of the snap) or in a unlikely event of a snapd
communication failure.

In other cases the command exits with status 0. In particular, an attempt to get
an unknown key does't raise an error and returns empty value.

FEATURES
========

Transactions
------------

Any configuration changes done from snap hooks are executed in transactions. In
case of hook failure, all the configuration changes are rolled back to the
original values.

Complex structures
------------------

Configuration values can store arbitrary JSON data. Such values can be set by
providing properly quoted JSON documents. Specific values contained in JSON
documents can be referenced by dotted paths. See examples below.

EXAMPLES
========

snapctl set username=frank email=frank@somehost.com
---------------------------------------------------

Set username and email values.

snapctl set user="{\\"name\\":\\"frank\\", \\"email\\":\\"frank@somehost.com\\"}"
---------------------------------------------------------------------------------

Set user to a JSON map.
 
snapctl set user.name=frank
---------------------------

Set name inside user document using dotted path (creates the map automatically if
it doesn't exist).

snapctl get user.name
---------------------

Get name from user document, prints just the value since it's a simple string:
`frank`

snapctl get -d user.name
------------------------

Get name from user document, forces JSON output resulting in:

::

 {
   "user.name": "frank"
 }


snapctl get user
----------------

Get user document, prints:

::

 {
   "name": "frank"
 }

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snapd/+filebug
