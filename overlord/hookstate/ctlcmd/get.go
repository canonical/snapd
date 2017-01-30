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

type getCommand struct {
	baseCommand

	// these two options are mutually exclusive
	ForceSlotSide bool `long:"slot" description:"return attribute values from the slot side of the connection"`
	ForcePlugSide bool `long:"plug" description:"return attribute values from the plug side of the connection"`

	Positional struct {
		PlugOrSlotSpec string   `positional-args:"true" positional-arg-name:":<plug|slot>"`
		Keys           []string `positional-arg-name:"<keys>" description:"option keys"`
	} `positional-args:"yes"`

	Document bool `short:"d" description:"always return document, even with single key"`
	Typed    bool `short:"t" description:"strict typing with nulls and quoted strings"`
}

var shortGetHelp = i18n.G("The get command prints configuration and interface connection settings.")
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

Values of interface connection settings may be printed with:

    $ snapctl get :myplug usb-vendor
    $ snapctl get :myslot path

This will return the named setting from the local interface endpoint, whether a plug
or a slot. Returning the setting from the connected snap's endpoint is also possible
by explicitly requesting that via the --plug and --slot command line options:

    $ snapctl get :myplug --slot usb-vendor

This requests the "usb-vendor" setting from the slot that is connected to "myplug".
`)

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() command {
		return &getCommand{}
	})
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
	if c.Positional.PlugOrSlotSpec == "" && len(c.Positional.Keys) == 0 {
		return fmt.Errorf(i18n.G("need option name or plug/slot and attribute name arguments"))
	}

	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot get without a context")
	}

	if c.Typed && c.Document {
		return fmt.Errorf("cannot use -d and -t together")
	}

	if strings.Contains(c.Positional.PlugOrSlotSpec, ":") {
		parts := strings.SplitN(c.Positional.PlugOrSlotSpec, ":", 2)
		snap, name := parts[0], parts[1]
		if name == "" {
			return fmt.Errorf("plug or slot name not provided")
		}
		if snap != "" {
			return fmt.Errorf(`"snapctl get %s" not supported, use "snapctl get :%s" instead`, c.Positional.PlugOrSlotSpec, parts[1])
		}

		return c.getInterfaceSetting(context, name)
	}

	// PlugOrSlotSpec is actually a configuration key.
	c.Positional.Keys = append([]string{c.Positional.PlugOrSlotSpec}, c.Positional.Keys[0:]...)
	c.Positional.PlugOrSlotSpec = ""

	return c.getConfigSetting(context)
}

func (c *getCommand) getConfigSetting(context *hookstate.Context) error {
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

func (c *getCommand) getInterfaceSetting(context *hookstate.Context, plugOrSlot string) error {
	// Make sure get :<plug|slot> is only supported during the execution of prepare-[plug|slot] hooks
	var isPreparePlugHook, isPrepareSlotHook, isConnectPlugHook, isConnectSlotHook bool

	if strings.HasPrefix(context.HookName(), "prepare-plug-") {
		isPreparePlugHook = true
	} else if strings.HasPrefix(context.HookName(), "connect-plug-") {
		isConnectPlugHook = true
	} else if strings.HasPrefix(context.HookName(), "prepare-slot-") {
		isPrepareSlotHook = true
	} else if strings.HasPrefix(context.HookName(), "connect-slot-") {
		isConnectSlotHook = true
	}
	if !(isPreparePlugHook || isPrepareSlotHook || isConnectPlugHook || isConnectSlotHook) {
		return fmt.Errorf(i18n.G("interface attributes can only be read during the execution of interface hooks"))
	}

	var err error
	var attributes map[string]map[string]interface{}

	context.Lock()
	defer context.Unlock()
	err = context.Get("attributes", &attributes)

	if err == state.ErrNoState {
		return fmt.Errorf(i18n.G("attributes not found"))
	}
	if err != nil {
		return err
	}

	if c.ForcePlugSide && c.ForceSlotSide {
		return fmt.Errorf("cannot use --plug and --slot together")
	}

	var snapName string
	isPlugSide := (isPreparePlugHook || isConnectPlugHook)
	isSlotSide := (isPrepareSlotHook || isConnectSlotHook)
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

	// check if the requested plug or slot is correct for this hook.
	var val string
	if err := context.Get("plug-or-slot", &val); err == nil {
		if val != plugOrSlot {
			return fmt.Errorf(i18n.G("unknown plug/slot %s"), plugOrSlot)
		}
	} else {
		return err
	}

	return c.printValues(func(key string) (interface{}, bool, error) {
		if value, ok := attributes[snapName][key]; ok {
			return value, true, nil
		}
		return nil, false, fmt.Errorf(i18n.G("unknown attribute %q"), key)
	})
}
