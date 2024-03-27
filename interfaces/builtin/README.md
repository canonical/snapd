# Interface policy

## Plug and slot rules

Declarative rules are used to control what plugs or slots a snap is
allowed to use, and if a snap is allowed to use a plug/slot, what other
slots/plugs can connect to that plug/slot on this snap.

The overall structure has two top-level keys: plugs and slots. These affect the
plugs and slots of the snap respectively. Beneath these are the names of
interfaces. Each interface key introduces a map with 6 possible keys:

- `allow-installation`
- `deny-installation`
- `allow-connection`
- `deny-connection`
- `allow-auto-connection`
- `deny-auto-connection`

Each of these keys can either have a static value of true/false or can be a
more complex object/list which is “evaluated” by snapd on a device to
determine the actual value, be it true or false.


### Base declaration and snap declarations

The rules defined in the snapd interfaces source code (via setting
`commonInterface.baseDeclarationSlots/Plugs`) form the
“base declaration” and can be overridden by per-snap rules in the
snap store published via the per-snap `snap-declaration` assertions,
which make it possible for a store to alter the policy hardcoded in
snapd. For example, a store assertion is typically used to grant
auto-connection to some plugs of a specific application where this is
deemed reasonable or safe (such as auto-connecting the `camera`
interface in a web-streaming application).

### Basic evaluation and precedence

When not otherwise specified, the default values for `allow-*` keys are `true`,
while the default values for `deny-*` keys are `false`. A matched `deny-*`
constraint overrides and takes precedence over a matching `allow-*`
constraint from within the same stanza. The order of evaluation of rules
is the following (execution stops as soon as a matching rule is found, meaning
that the topmost elements in this list have higher priority):

- `deny-*` keys in plug snap-declaration rule
- `allow-*` keys in plug snap-declaration rule
- `deny-*` keys in slot snap-declaration rule
- `allow-*` keys in slot snap-declaration rule
- `deny-*` keys in plug base-declaration rule
- `allow-*` keys in plug base-declaration rule
- `deny-*` keys in slot base-declaration rule
- `allow-*` keys in slot base-declaration rule

In other words, snap-declaration (store) rules have priority over
base-declaration rules; then, plug rules have priority over slot rules, and
finally, deny rules have priority over allow rules within the same stanza.

### allow-installation

The `allow-installation` key is evaluated when the snap is being installed. If
this evaluates to false, the snap cannot be installed if an interface plug or
slot for which `allow-installation` evaluated to `false` exists in the snap. An
example would be the `snapd-control` interface, which has in the
base-declaration the static `allow-installation: false` rule for plugs:

    snapd-control:
      allow-installation: false
      deny-auto-connection: true

If a snap does not plug `snapd-control` then this rule does not apply, but if
the snap does declare a `snapd-control` plug and there are no assertions in the
store for this snap about allowing `snapd-control`, then snap installation will
fail.

Snap interfaces that have `allow-installation` set to `false` for their plugs
in the base-declaration are said to be **super-privileged**, meaning they
cannot be used at all without a snap-declaration assertion.

A snap's interface slot provided by a non-system snap is considered
**super-privileged** if it has `allow-installation` that evaluates to `false`
in the base-declaration. An application snap or gadget defining such slots
cannot be used without an accompanying snap-declaration assertion.

### allow-connection

The `allow-connection` key controls whether an API/manual connection
is permitted at all and usually is used to ensure that only
“compatible” plugs and slots are connected to each other. A great
example is the content interface, where the following (abbreviated)
rule from the base-declaration is used to ensure that a candidate plug
and slot content interface have matching `content` attribute values:

    allow-connection:
      plug-attributes:
        content: $SLOT(content)

This can be read as `allow-connection` evaluating to `true` only when the plug
has an attribute `content` with the same value as the attribute `content` in
the slot. That is to say, these plug and slots are compatible because `content`
does match for the plug and slot:

    # in the snap providing the content:
    slots:
      foo-content:
        interface: content
        content: specific-files

    # in the snap consuming the content:
    plugs:
      foo-content:
        interface: content
        content: specific-files

While the following plug and slots are not compatible:

    slots:
      foo-content:
        interface: content
        content: other-files

    plugs:
      foo-content:
        interface: content
        content: specific-files


### allow-auto-connection

The allow-auto-connection key is the final key considered when snapd is
evaluating the automatic connection of interface plugs and slots. If this key
evaluates to `true`, then this plug/slot combination is considered a valid
candidate for automatic connection. In this context allow-connection is ignored.

An automatic connection will happen normally only if there is one single
candidate combination with a slot for a given plug.


### Supported rule constraints

Each of the keys seen before (`allow/deny-installation`,
`allow/deny-connection`, and `allow/deny-auto-connection`) has a set of
sub-keys that can be used as rules with each constraint. The authoritative
place where this information comes from is inside snapd in the `asserts`
package, specifically the file `ifacedecls.go` is the main place where these
are defined.

In `allow-connection` or `allow-auto-connection` constraints about snap type,
snap ID and publisher can only be specified for the other side snap (e.g. a
slot-side `allow-connection` constraint can only specify `plug-snap-type`,
`plug-snap-id`, `plug-snap-publisher`).
As an exception, constraints on snap type for the slot providing snap
(`slot-snap-type`) can be specified on the slot side as well. This is only
meaningful/useful in the base declaration as it allows for a constraint on
whether the slot side is provided by the system snap or not.

For the `plug-snap-type` and `slot-snap-type` rules there are 4
possible values: `core`, `gadget`, `kernel`, and `app`. The `core` snap
type refers to whichever snap is providing snapd on the system and
therefore the system interface slots, either the `core` snap or `snapd`
snap (typically `core` snap on UC16 devices, `snapd` snap on UC18+
systems, and either on classic systems depending on re-exec logic).

The `on-store`, `on-brand`, and `on-model` rules are generally not hard-coded
within snapd interfaces. They are instead specified in store assertions where
they are known as "device context constraints". These device context
constraints are primarily used to ensure a given rule only applies to a device
with a serial assertion (and thus model assertion) from a given brand or using
a given store (as specified by the model). This is because if the assertion and
snap from a brand store were copied to a non-branded device, the assertion
could still be acknowledged by the device and the snap installed, but the
assertion would not operate, and snap connections would not take place as they
do on the branded device.

The `plug-names` and `slot-names` rules are also only used in store assertions.
They refer to the naming of a plug or slot when that slot is scoped globally
with a name other than the generic interface name.
For example this assertion:

    plugs:
      gpio:
        allow-auto-connection:
          slot-names: [ gpio1 ]
          plug-names: [ gpio-red-led ]

only allows the plugging snap to have its plug named `gpio-red-led`
auto-connected to a gpio slot named `gpio1`.


### Rule evaluation

#### Greedy connection / single slot rule

The first rule about whether an automatic connection happens between a plug and
a slot has to do with “arity” or how many slots a given plug is being
considered to connect to and vice versa. This is expressed with the
`slots-per-plug` and `plugs-per-slot` rules, with the default value of
`plugs-per-slot` being “`*`” meaning any number of plugs can be connected to a
specific slot. The default value of `slots-per-plug` is "`1`", however, meaning
that a plug can in general without a special snap-declaration only
automatically connect to one slot. All that is to say, if there are multiple
candidate slots, in the default case a plug will auto-connect to neither of
them and snapd will issue a warning.

See also [this forum
post](https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438)
which was written when this logic was first implemented.


#### Maps and Lists

The next rule about evaluating snap-declaration assertions is that maps are
treated as logical ANDs where each key in the map must individually evaluate to
`true` and lists are treated as logical ORs where only one of the elements of
the list must evaluate to `true`. With the following example assertion,
order
for the `serial-port` plug in this snap to auto-connect, either the first
element of the list must evaluate to `true` or the second element of the list
must evaluate to `true` (or they could both theoretically evaluate to `true`,
but this is impossible in practice since the slots can only come from gadgets
and so to match both the system would have to have two gadget snaps installed
simultaneously which is impossible).

    plugs:
      serial-port:
        allow-auto-connection:
          -
            on-store:
              - my-app-store
            plug-names:
              - serial-rf-nic
            slot-attributes:
              path: /dev/serial-port-rfnic
            slot-names:
              - serial-rf-nic
            slot-snap-id:
              - Cx4J8ADDq8xULNaAjO7mQid75ru4rObB
          -
            on-store:
              - my-app-store
            plug-names:
              - serial-rf-nic
            slot-attributes:
              path: /dev/serial-port-rfnic
            slot-names:
              - serial-rf-nic
            slot-snap-id:
              - WabnwLoV48BCMj8NoOetmdxFFMxDsPGb

For the first element of the allow-auto-connection list to evaluate to
`true`, the following things must be true:

- The device must be on a brand device in `my-app-store` AND
- The plug name must be `serial-rf-nic` AND
- The slot name must be `serial-rf-nic` AND
- The slot must declare an attribute, `path`, with the value
  `/dev/serial-port-rfnic` AND
- The slot must come from a snap with a snap ID of
  `Cx4J8ADDq8xULNaAjO7mQid75ru4rObB`

The above is also true for the second element of the allow-auto-connection list.

An equivalent way to write an assertion that works in exactly the same way would be:

    plugs:
      serial-port:
        allow-auto-connection:
          on-store:
            - my-app-store
          plug-names:
            - serial-rf-nic
          slot-attributes:
            path: /dev/serial-port-rfnic
          slot-names:
            - serial-rf-nic
          slot-snap-id:
            - Cx4J8ADDq8xULNaAjO7mQid75ru4rObB
            - WabnwLoV48BCMj8NoOetmdxFFMxDsPGb

The chief difference here is that instead of having a list of two maps, we
instead have a single map, and the key which changes for the two gadgets is the
`slot-snap-id` which now has two values in a list. In this case, the slot snap
ID must be one of the snap IDs in the list in order for the `slot-snap-id` rule
to evaluate to true. So the following things must be true:

- The device must be on a brand device in `my-app-store` AND
- The plug name must be `serial-rf-nic` AND
- The slot-name must be `serial-rf-nic` AND
- The slot must declare an attribute, `path`, with the value
  `/dev/serial-port-rfnic` AND
- The slot must come from a snap with a snap ID of any of the following elements:
  - `Cx4J8ADDq8xULNaAjO7mQid75ru4rObB`, OR
  - `WabnwLoV48BCMj8NoOetmdxFFMxDsPGb`

Lists and maps can also be used as values for attributes under
plug/slot-attributes constraints. A map will match only if the attribute value
contains all the entries in the constraints map with the same values (extra
attribute elements are ignored).
A list will match against a non-list attribute value if the value matches any
of the list elements. A list will match against a list attribute value if all
the elements in the attribute list value in turn match something in the list.
This means, for example, a constraint list of value constraints will match a list
of attribute scalars if the two groups of values match as a set (order doesn't
matter).


#### Attribute constraints and special matches and variables

Plug/slot-attributes string value constraints are interpreted as regexps
(wrapped implicitly in `^$` anchors), unless they are one of the special forms
starting with `$`.

The special forms starting with `$` currently consist of:

- `$SLOT_PUBLISHER_ID`, `$PLUG_PUBLISHER_ID`: these can be specified in the
  `plug-publisher-id` and `slot-publisher-id` constraints respectively and are
  used from the plug side and slot side of a declaration to refer to the
  publisher-id of the other side of the connection.
- `$PLUG()`, `$SLOT()`: similar to the above, but used in the `plug-attributes`
  and `slot-attributes` constraints for specifying attributes instead of
  publisher-ids.
- `$MISSING`: used in the `plug-attributes` and `slot-attributes` constraints
  to match when the attribute set to `$MISSING` is not specified in the snap.

For example, these features are used in the base-declaration for the `content`
interface to express that a connection is only allowed when the attribute value
of the verbatim “content” attribute on the slot side is the same as the plug
side and that auto-connection should only take place by default when the plug
publisher ID is the same as the slot publisher ID (unless this is overridden by
a store assertion):

    content:
      allow-installation:
        slot-snap-type:
          - app
          - gadget
      allow-connection:
        plug-attributes:
          content: $SLOT(content)
      allow-auto-connection:
        plug-publisher-id:
          - $SLOT_PUBLISHER_ID
        plug-attributes:
          content: $SLOT(content)

## Unasserted local installation

When a snap is installed without matching assertions using `--dangerous`, many
checks are not performed. This helps with local snap development.

For installation only, any slot-snap-type constraint in a base-declaration
`allow-installation` rule for slots is checked. Any `deny-installation`
rule here is ignored. Nothing is checked for the snap plugs.

For connections involving the snap, no rules are checked and it is always
allowed.

For auto-connection, the base-declaration and any rule for the other side (if
available) is considered. This usually means no auto-connection will happen
unless allowed by the base-declaration or by a slot-side snap-declaration.

## Base declaration policy patterns

This section discusses the patterns to follow when writing base declaration
rules for an interface (via setting
`commonInterface.baseDeclarationSlots/Plugs`), depending on the security
characteristics of the interface.

Slots for a specific interface can be provided by the system snap (so called
implicit slots), or by an application snap, or sometimes by both.

For almost all interfaces, and whenever possible, the base declaration rules are
written as slot-side-only rules.


Importantly, items are not merged between the slots and plugs, or between the base declaration and snap declaration for a particular type of rule.

This means that if a connection rule for both the slot-side and plug-side is specified in the base declaration, only the plug side is used (plug-side overrides the slot side).


Installation rules target their side only; a slot-side installation rule is for
allowing snaps with a slot of the given interface. A plug-side installation
rule is for snaps with a plug of the given interface.

Interfaces can be broadly categorized as:

- auto-connected implicit slots (eg, network)
- manually connected implicit slots (eg, bluetooth-control)
- auto-connected app-provided slots  (eg, mir)
- manually connected app-provided slots (eg, bluez)

As such, that they will follow this pattern:

    slots:
      auto-connected-implicit-slot:
        allow-installation:
          slot-snap-type:
            - core                     # implicit slot
        #allow-auto-connection: true   # allow auto-connect, default

      manual-connected-implicit-slot:
        allow-installation:
          slot-snap-type:
            - core                     # implicit slot
        deny-auto-connection: true     # force manual connect

      auto-connected-provided-slot:
        allow-installation:
          slot-snap-type:
            - app                      # app provided slot
        deny-connection: true          # require allow-connection in snap decl

      manual-connected-provided-slot:
        allow-installation:
          slot-snap-type:
            - app                      # app provided slot
        deny-connection: true          # require allow-connection in snap decl
        deny-auto-connection: true     # force manual connect

As some slots can be provided by both the system or an application snap, some interfaces will be in more than one category.

Auto-connection should be allowed in the base-declaration if the access to the
resources provided by the interface does not have security implications.

App-provided slots use 'deny-connection: true' since slot implementations
require privileged access to the system and the snap must be trusted. In this
manner, a snap declaration for the slot-providing snap is required to override
the base declaration to allow connections with the app-provided slot.

Slots dealing with hardware will typically specify 'gadget' and 'core' as
the slot-snap-type (eg, serial-port). Eg:

    slots:
      manual-connected-hw-slot:
        allow-installation:
          slot-snap-type:
            - core
            - gadget
        deny-auto-connection: true

Denying auto-connection not only stops access but also covers the fact that
there might be multiple slots for an hardware interface for which there is no
way to choose one.

So called super-privileged plugs should also disallow installation on a
system. A snap declaration is required to override the base declaration to
allow installation (eg, kernel-module-control). Eg:

    plugs:
      manual-connected-super-privileged-plug:
        allow-installation: false
        deny-auto-connection: true
    (remember this overrides slot side rules)

This pattern makes sense for interfaces that carry great security risks and
allow the snap to take control outside of the sandbox, e.g. installing a kernel
module.

So called super-privileged slot implementations should also disallow
installation on a system and a snap declaration is required to override the
base declaration to allow installation (eg, docker). Eg:

    slots:
      manual-connected-super-privileged-slot:
        allow-installation: false
        deny-connection: true
        deny-auto-connection: true

This pattern makes sense for interfaces where implementing them requires
extensive system access, or where there is the need for a review to check for
policy or to avoid resource use/naming clashes.

Some interfaces have policy that is meant to cover application snap slot
implementations but also classic systems. Since the slot implementation is
privileged, we require a snap declaration to be used for app-provided slot
implementations on non-classic systems (eg, `network-manager`). Eg:

    slots:
      classic-or-not-slot:
        allow-installation:
          slot-snap-type:
            - app
            - core
        deny-auto-connection: true
        deny-connection:
          on-classic: false

The idea of this pattern is that on classic we expect the slot to be system
provided and for it to be app-provide on Core. However, the work on Core
Desktop means this idea is not always valid. A different approach is to
deny-connection based on the type of slot carrying snap, e.g for
`upower-observe` nowadays, we have:

    upower-observe:
      allow-installation:
        slot-snap-type:
          - app
          - core
      deny-auto-connection:
        slot-snap-type:
          - app
      deny-connection:
        slot-snap-type:
          - app

Some interfaces only have implicit slots and should be auto-connected only on classic systems (eg, home). Eg:

    slots:
      implicit-auto-classic-slot:
        allow-installation:
          slot-snap-type:
            - core
        deny-auto-connection:
          on-classic: false

Some interfaces with app-provided slots allow connections if some expectation
is met.  This in turn is expressed by some kind of tag attribute on both plug
and slot (usually named after the interface) that must match, for example
`content`:

    slots:
      content:
        allow-installation:
          slot-snap-type:
            - app
            - gadget
        allow-connection:
          plug-attributes:
            content: $SLOT(content)
        allow-auto-connection:
          plug-publisher-id:
            - $SLOT_PUBLISHER_ID
          plug-attributes:
            content: $SLOT(content)

In these situations auto-connection is often granted by default if the
publisher also matches.

Some interfaces might need complex policies that mix many of these patterns, for example, shared-memory:

- allows auto-connection to the implicit slot if the plug set a `private`
  attribute to `true` (default is false)
- supports app-provided slots but that are super-privileged to avoid naming
  clashes
- uses a tag attribute to match plug and slots, and if the slot could be defined
  lets same publisher plugs auto-connect

```
 slots:
   shared-memory:
     allow-installation:
       slot-snap-type:
         - app
         - gadget
         - core
     deny-installation:
       slot-snap-type:
         - app
         - gadget
     deny-auto-connection: true

 plugs:
  shared-memory:
     allow-connection:
       -
         plug-attributes:
           private: false
         slot-attributes:
           shared-memory: $PLUG(shared-memory)
       -
         plug-attributes:
           private: true
         slot-snap-type:
           - core
     allow-auto-connection:
       -
         plug-attributes:
           private: false
         slot-publisher-id:
           - $PLUG_PUBLISHER_ID
         slot-attributes:
           shared-memory: $PLUG(shared-memory)
       -
         plug-attributes:
           private: true
         slot-snap-type:
           - core
```

Note the combining of `allow-installation` and `deny-installation`
`slot-snap-type` constraints to make the slot super-privileged while accounting
for the slot snap type check done for `--dangerous` installations.
