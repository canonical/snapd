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
)

// SnapAndName holds a snap name and a plug or slot name.
// TODO move this somewhere else to share the code with main cmd
type SnapAndName struct {
	Snap string `positional-arg-name:"<snap>"`
	Name string
}

// UnmarshalFlag unmarshals snap and plug or slot name.
// TODO move this somewhere else to share the code with main cmd
func (sn *SnapAndName) UnmarshalFlag(value string) error {
	parts := strings.Split(value, ":")
	sn.Snap = ""
	sn.Name = ""
	switch len(parts) {
	case 1:
		sn.Snap = parts[0]
	case 2:
		sn.Snap = parts[0]
		sn.Name = parts[1]
		// Reject "snap:" (that should be spelled as "snap")
		if sn.Name == "" {
			sn.Snap = ""
		}
	}
	if sn.Snap == "" && sn.Name == "" {
		return fmt.Errorf(i18n.G("invalid value: %q (want snap:name or snap)"), value)
	}
	return nil
}

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

func (c *getCommand) Execute(args []string) error {
	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot get without a context")
	}

	if c.Typed && c.Document {
		return fmt.Errorf("cannot use -d and -t together")
	}

	var snapAndPlugOrSlot SnapAndName
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

	patch := make(map[string]interface{})
	context.Lock()
	transaction := configstate.ContextTransaction(context)
	context.Unlock()

	for _, key := range c.Positional.Keys {
		var value interface{}
		err := transaction.Get(c.context().SnapName(), key, &value)
		if err == nil {
			patch[key] = value
		} else if configstate.IsNoOption(err) {
			if !c.Typed {
				value = ""
			}
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

func (c *getCommand) handleGetInterfaceAttributes(context *hookstate.Context, snapName string, plugOrSlot string) error {
	var err error
	var attributes map[string]map[string]interface{}

	context.Lock()
	err = context.Get("attributes", &attributes)
	context.Unlock()

	if err == state.ErrNoState {
		return fmt.Errorf(i18n.G("no attributes found"))
	}
	if err != nil {
		return err
	}

	if c.ForcePlugSide && c.ForceSlotSide {
		return fmt.Errorf("cannot use --plug and --slot together")
	}

	if snapName == "" {
		// snap name not given, default to the other end of the interface connection unless
		// --slot or --plug argument is provided.
		isPlugSide := (strings.HasPrefix(context.HookName(), "prepare-plug-") || strings.HasPrefix(context.HookName(), "connect-plug-"))
		isSlotSide := (strings.HasPrefix(context.HookName(), "prepare-slot-") || strings.HasPrefix(context.HookName(), "connect-slot-"))
		if (c.ForcePlugSide && isPlugSide) || (c.ForceSlotSide && isSlotSide) {
			// get own attributes
			snapName = context.SnapName()
		} else {
			// get attributes of the remote end
			err = context.Get("other-snap", &snapName)
			if err != nil {
				return fmt.Errorf(i18n.G("failed to get the other snap name from hook context: %q"), err)
			}
		}
	}

	attrsToPrint := make(map[string]interface{})
	for _, key := range c.Positional.Keys {
		if value, ok := attributes[snapName][key]; ok {
			attrsToPrint[key] = value
		} else {
			return fmt.Errorf(i18n.G("unknown attribute %q"), key)
		}
	}

	if len(attrsToPrint) == 1 {
		if val, ok := attrsToPrint[c.Positional.Keys[0]].(string); ok {
			c.printf("%s\n", val)
			return nil
		}
	}

	var bytes []byte
	bytes, err = json.MarshalIndent(attrsToPrint, "", "\t")
	if err != nil {
		return err
	}

	c.printf("%s\n", string(bytes))

	return nil
}
