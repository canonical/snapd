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

**Note:** The development of specific hooks is ongoing. None are currently
supported.
