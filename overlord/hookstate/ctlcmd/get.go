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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
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
		return fmt.Errorf(i18n.G("get which option?"))
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
		if config.IsNoOption(err) {
			if !c.Typed {
				value = ""
			}
			return value, false, nil
		}
		return value, false, err
	})
}

type ifaceHookType int

const (
	preparePlugHook ifaceHookType = iota
	prepareSlotHook
	unpreparePlugHook
	unprepareSlotHook
	connectPlugHook
	connectSlotHook
	disconnectPlugHook
	disconnectSlotHook
	unknownHook
)

func interfaceHookType(hookName string) (ifaceHookType, error) {
	switch {
	case strings.HasPrefix(hookName, "prepare-plug-"):
		return preparePlugHook, nil
	case strings.HasPrefix(hookName, "connect-plug-"):
		return connectPlugHook, nil
	case strings.HasPrefix(hookName, "prepare-slot-"):
		return prepareSlotHook, nil
	case strings.HasPrefix(hookName, "connect-slot-"):
		return connectSlotHook, nil
	case strings.HasPrefix(hookName, "disconnect-plug-"):
		return disconnectPlugHook, nil
	case strings.HasPrefix(hookName, "disconnect-slot-"):
		return disconnectSlotHook, nil
	case strings.HasPrefix(hookName, "unprepare-slot-"):
		return unprepareSlotHook, nil
	case strings.HasPrefix(hookName, "unprepare-plug-"):
		return unpreparePlugHook, nil
	default:
		return unknownHook, fmt.Errorf("unknown hook type")
	}
}

func validatePlugOrSlot(attrsTask *state.Task, plugSide bool, plugOrSlot string) error {
	// check if the requested plug or slot is correct for given hook.
	attrsTask.State().Lock()
	defer attrsTask.State().Unlock()

	var name string
	var err error
	if plugSide {
		var plugRef interfaces.PlugRef
		if err = attrsTask.Get("plug", &plugRef); err == nil {
			name = plugRef.Name
		}
	} else {
		var slotRef interfaces.SlotRef
		if err = attrsTask.Get("slot", &slotRef); err == nil {
			name = slotRef.Name
		}
	}
	if err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot find plug or slot data in the appropriate task"))
	}
	if name != plugOrSlot {
		return fmt.Errorf(i18n.G("unknown plug or slot %q"), plugOrSlot)
	}
	return nil
}

func attributesTask(context *hookstate.Context) (*state.Task, error) {
	var attrsTaskID string
	context.Lock()
	defer context.Unlock()

	if err := context.Get("attrs-task", &attrsTaskID); err != nil {
		return nil, err
	}

	st := context.State()

	attrsTask := st.Task(attrsTaskID)
	if attrsTask == nil {
		return nil, fmt.Errorf(i18n.G("internal error: cannot find attrs task"))
	}

	return attrsTask, nil
}

func (c *getCommand) getInterfaceSetting(context *hookstate.Context, plugOrSlot string) error {
	// Make sure get :<plug|slot> is only supported during the execution of interface hooks
	hookType, err := interfaceHookType(context.HookName())
	if err != nil {
		return fmt.Errorf(i18n.G("interface attributes can only be read during the execution of interface hooks"))
	}

	var attrsTask *state.Task
	attrsTask, err = attributesTask(context)
	if err != nil {
		return err
	}

	if c.ForcePlugSide && c.ForceSlotSide {
		return fmt.Errorf("cannot use --plug and --slot together")
	}

	isPlugSide := (hookType == preparePlugHook || hookType == unpreparePlugHook || hookType == connectPlugHook || hookType == disconnectPlugHook)
	if err = validatePlugOrSlot(attrsTask, isPlugSide, plugOrSlot); err != nil {
		return err
	}

	var which string
	if c.ForcePlugSide || (isPlugSide && !c.ForceSlotSide) {
		which = "plug"
	} else {
		which = "slot"
	}

	st := context.State()
	st.Lock()
	defer st.Unlock()

	var staticAttrs, dynamicAttrs map[string]interface{}
	if err = attrsTask.Get(which+"-static", &staticAttrs); err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get %s from appropriate task"), which)
	}
	if err = attrsTask.Get(which+"-dynamic", &dynamicAttrs); err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get %s from appropriate task"), which)
	}

	return c.printValues(func(key string) (interface{}, bool, error) {
		subkeys, err := config.ParseKey(key)
		if err != nil {
			return nil, false, err
		}

		var value interface{}
		err = config.GetFromChange(context.SnapName(), subkeys, 0, staticAttrs, &value)
		if err == nil {
			return value, true, nil
		}
		if config.IsNoOption(err) {
			err = config.GetFromChange(context.SnapName(), subkeys, 0, dynamicAttrs, &value)
			if err == nil {
				return value, true, nil
			}
			if config.IsNoOption(err) {
				return nil, false, fmt.Errorf(i18n.G("unknown attribute %q"), key)
			}
		}
		return nil, false, err
	})
}
