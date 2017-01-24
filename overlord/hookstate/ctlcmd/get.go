// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package ctlcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type getCommand struct {
	baseCommand

	// these two options are mutually exclusive
	ForceSlotSide bool `long:"slot" description:"request attribute of the slot"`
	ForcePlugSide bool `long:"plug" description:"request attribute of the plug"`

	Positional struct {
		PlugOrSlotSpec string   `positional-args:"true" positional-arg-name:"<snap>:<plug|slot>" required:"yes"`
		Keys           []string `positional-arg-name:"<keys>" description:"option keys"`
	} `positional-args:"yes"`

	Document bool `short:"d" description:"always return document, even with single key"`
	Typed    bool `short:"t" description:"strict typing with nulls and quoted strings"`
}

var shortGetHelp = i18n.G("Prints configuration options or attribute values of plug/slot")
var longGetHelp = i18n.G(`
The get command prints configuration options for the current snap.

    $ snapctl get username
    frank

If multiple option names are provided, a document is returned:

    $ snapctl get username password
    {
        "username": "frank",
        "password": "..."
    }

Nested values may be retrieved via a dotted path:

    $ snapctl get author.name
    frank

Values of plug or slot attributes may be printed in plug hooks via:

    $ snapctl get :plugname serial-path
	/dev/ttyS0

	$ snapctl get --slot :slotname camera-path
	/dev/video0

Values of plug or slot attributes may be printed in slot hooks via:

    $ snapctl get --plug :slotname serial-path
	/dev/ttyS0

	$ snapctl get :slotname camera-path
	/dev/video0
`)

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() command {
		return &getCommand{}
	}, map[string]string{
		"slot": i18n.G("Access the slot side of the connection"),
		"plug": i18n.G("Access the plug side of the connection"),
	}, []argDesc{
		{name: i18n.G("<snap>:<slot>")}})
}

func (c *getCommand) printValues(getByKey func(string) (interface{}, bool, error)) error {
	patch := make(map[string]interface{})
	for _, key := range c.Positional.Keys {
		value, output, err := getByKey(key)
		if err == nil {
			if output {
				patch[key] = value
			} // else skip this value
		} else {
			return err
		}
	}

	var confToPrint interface{} = patch
	if !c.Document && len(c.Positional.Keys) == 1 {
		confToPrint = patch[c.Positional.Keys[0]]
	}

	if c.Typed && confToPrint == nil {
		c.printf("null\n")
		return nil
	}

	if s, ok := confToPrint.(string); ok && !c.Typed {
		c.printf("%s\n", s)
		return nil
	}

	var bytes []byte
	if confToPrint != nil {
		var err error
		bytes, err = json.MarshalIndent(confToPrint, "", "\t")
		if err != nil {
			return err
		}
	}

	c.printf("%s\n", string(bytes))

	return nil
}

func (c *getCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot get without a context")
	}

	if c.Typed && c.Document {
		return fmt.Errorf("cannot use -d and -t together")
	}

	var snapAndPlugOrSlot snap.SnapAndName
	// treat PlugOrSlotSpec argument as config key if it doesn't contain ':'
	if !strings.Contains(c.Positional.PlugOrSlotSpec, ":") {
		c.Positional.Keys = append([]string{c.Positional.PlugOrSlotSpec}, c.Positional.Keys[0:]...)
		c.Positional.PlugOrSlotSpec = ""
	} else {
		snapAndPlugOrSlot.UnmarshalFlag(c.Positional.PlugOrSlotSpec)
	}

	if snapAndPlugOrSlot.Name != "" {
		return c.handleGetInterfaceAttributes(context, snapAndPlugOrSlot.Snap, snapAndPlugOrSlot.Name)
	}

	if c.ForcePlugSide || c.ForceSlotSide {
		return fmt.Errorf("cannot use --plug or --slot without <snap>:<plug|slot> argument")
	}

	context.Lock()
	transaction := configstate.ContextTransaction(context)
	context.Unlock()

	return c.printValues(func(key string) (interface{}, bool, error) {
		var value interface{}
		err := transaction.Get(c.context().SnapName(), key, &value)
		if err == nil {
			return value, true, nil
		}
		if configstate.IsNoOption(err) {
			if !c.Typed {
				value = ""
			}
			return value, false, nil
		}
		return value, false, err
	})
}

func (c *getCommand) handleGetInterfaceAttributes(context *hookstate.Context, snapName string, plugOrSlot string) error {
	var err error
	var attributes map[string]map[string]interface{}

	context.Lock()
	defer context.Unlock()
	err = context.Get("attributes", &attributes)

	if err == state.ErrNoState {
		return fmt.Errorf(i18n.G("no attributes found"))
	}
	if err != nil {
		return err
	}

	if c.ForcePlugSide && c.ForceSlotSide {
		return fmt.Errorf("cannot use --plug and --slot together")
	}

	// the typical case, we don't expect snap name to be provided via snapctl get :<plug|slot> ...
	// if it's provided it should be the current snap, otherwise it's an error.
	if snapName == "" {
		isPlugSide := (strings.HasPrefix(context.HookName(), "prepare-plug-") || strings.HasPrefix(context.HookName(), "connect-plug-"))
		isSlotSide := (strings.HasPrefix(context.HookName(), "prepare-slot-") || strings.HasPrefix(context.HookName(), "connect-slot-"))
		if (isSlotSide && c.ForcePlugSide) || (isPlugSide && c.ForceSlotSide) {
			// get attributes of the remote end
			err = context.Get("other-snap", &snapName)
			if err != nil {
				// this should never happen unless the context is inconsistent
				return fmt.Errorf(i18n.G("failed to get the name of the other snap from hook context: %q"), err)
			}
		} else {
			// get own attributes
			snapName = context.SnapName()
		}
	} else {
		// support the unlikely case where snap name was provided but it's not the current snap.
		if snapName != context.SnapName() {
			return fmt.Errorf(i18n.G("snap name other than current snap cannot be used"))
		}
	}

	return c.printValues(func(key string) (interface{}, bool, error) {
		if value, ok := attributes[snapName][key]; ok {
			return value, true, nil
		}
		return nil, false, fmt.Errorf(i18n.G("unknown attribute %q"), key)
	})
}
