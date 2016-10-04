# Hooks

There are a number of situations where snapd needs to notify a snap that
something has happened. For example, when a snap is upgraded, it may need to run
some sort of migration on the previous version's data in order to make it
consumable by the new version. Or when an interface is connected or
disconnected, the snap might need to obtain attributes specific to that
connection. These types of situations are handled by hooks.

A hook is defined as an executable contained within the `meta/hooks/` directory
inside the snap. The file name of the executable is the name of the hook (e.g.
the upgrade hook executable would be `meta/hooks/upgrade`).

As long as the file name of the executable corresponds to a supported hook name,
that's all one needs to do in order to utilize a hook within their snap. Note
that hooks, like apps, are executed within a confined environment. By default
hooks will run with no plugs; if a hook needs more privileges one can use the
top-level attribute `hooks` in `snap.yaml` to request plugs, like so:

    hooks: # Top-level YAML attribute, parallel to `apps`
        upgrade: # Hook name, corresponds to executable name
            plugs: [network] # Or any other plugs required by this hook

Note that hooks will be called with no parameters. If they need more information
from snapd (or need to provide information to snapd) they can utilize the
`snapctl` command (for more information on `snapctl`, see `snapctl -h`).


## Supported Hooks

**Note:** The development of specific hooks is ongoing.


### `configure`

The `configure` hook will be called whenever the user requests a configuration
change via the `snap set` command. The hook should use `snapctl get` to retrieve
the requested configuration from snapd, and act upon it. If it exits non-zero,
the configuration will not be applied.


#### `configure` example

Say the user runs:

```bash
$ snap set <snapname> username=foo password=bar
```

The `configure` hook would be located within the snap at `meta/hooks/configure`.
An example of what it might contain is:

```bash
#!/bin/sh

if ! username=$(snapctl get username); then
    echo "Username is required"
    exit 1
fi

if ! password=$(snapctl get password); then
    echo "Password is required"
    exit 1
fi

# Handle username and password, perhaps write to a credential file of some sort.
echo "user=$username" > $SNAP_DATA/credentials
echo "password=$password" >> $SNAP_DATA/credentials
chmod 600 $SNAP_DATA/credentials
```

### `prepare-device` (gadget hook)

The optional `prepare-device` hook will be called on the gadget if
present at the start fo the device initialisation process, once the
device has first booted and the gadget snap has been installed. The
hook will also be called if this process is retried later from scratch
in case of initialisation failures.

The device initialisation process is for example responsible of
setting the serial indentification of the device through an exchange
with a device service. The `prepare-device` hook can for example
redirect this exchange and dynamically set options relevant to it.

#### `prepare-device` example

```bash
#!/bin/sh

# optionally set the url of the service
snapctl set device-service.url="https://device-service"
# set optional extra HTTP headers for requests to the service
snapctl set device-service.headers='{"token": "TOKEN"}'

# set an optional proposed serial identifier, depending on the service
# this can end up being ignored;
# this might need to be obtained dynamically
snapctl set registration.proposed-serial="DEVICE-SERIAL"

# optionally pass details of the device as the body of registration request,
# the body is text, typically YAML;
# this might need to be obtained dynamically
snapctl set registration.body='mac: "00:00:00:00:ff:00"'

```
