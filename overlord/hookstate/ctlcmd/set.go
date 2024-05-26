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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
)

type setCommand struct {
	baseCommand

	Positional struct {
		PlugOrSlotSpec string   `positional-arg-name:":<plug|slot>"`
		ConfValues     []string `positional-arg-name:"key=value"`
	} `positional-args:"yes"`

	String bool `short:"s" description:"parse the value as a string"`
	Typed  bool `short:"t" description:"parse the value strictly as JSON document"`
}

var (
	shortSetHelp = i18n.G("Set either configuration options or interface connection settings")
	longSetHelp  = i18n.G(`
The set command sets the provided configuration options as requested.

    $ snapctl set username=frank password=$PASSWORD

All configuration changes are persisted at once, and only after the hook
returns successfully.

Nested values may be modified via a dotted path:

    $ snapctl set author.name=frank

Configuration option may be unset with exclamation mark:
    $ snapctl set author!

Plug and slot attributes may be set in the respective prepare and connect hooks
by naming the respective plug or slot:

    $ snapctl set :myplug path=/dev/ttyS0
`)
)

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} })
}

func (s *setCommand) Execute(args []string) error {
	if s.Positional.PlugOrSlotSpec == "" && len(s.Positional.ConfValues) == 0 {
		return fmt.Errorf(i18n.G("set which option?"))
	}

	context := mylog.Check2(s.ensureContext())

	if s.Typed && s.String {
		return fmt.Errorf("cannot use -t and -s together")
	}

	// treat PlugOrSlotSpec argument as key=value if it contains '=' or doesn't contain ':' - this is to support
	// values such as "device-service.url=192.168.0.1:5555" and error out on invalid key=value if only "key" is given.
	if strings.Contains(s.Positional.PlugOrSlotSpec, "=") || !strings.Contains(s.Positional.PlugOrSlotSpec, ":") {
		s.Positional.ConfValues = append([]string{s.Positional.PlugOrSlotSpec}, s.Positional.ConfValues[0:]...)
		s.Positional.PlugOrSlotSpec = ""
		return s.setConfigSetting(context)
	}

	parts := strings.SplitN(s.Positional.PlugOrSlotSpec, ":", 2)
	snap, name := parts[0], parts[1]
	if name == "" {
		return fmt.Errorf("plug or slot name not provided")
	}
	if snap != "" {
		return fmt.Errorf(`"snapctl set %s" not supported, use "snapctl set :%s" instead`, s.Positional.PlugOrSlotSpec, parts[1])
	}
	return s.setInterfaceSetting(context, name)
}

func (s *setCommand) setConfigSetting(context *hookstate.Context) error {
	context.Lock()
	tr := configstate.ContextTransaction(context)
	context.Unlock()

	for _, patchValue := range s.Positional.ConfValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) == 1 && strings.HasSuffix(patchValue, "!") {
			key := strings.TrimSuffix(patchValue, "!")
			tr.Set(s.context().InstanceName(), key, nil)
			continue
		}
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), patchValue)
		}
		key := parts[0]

		var value interface{}
		if s.String {
			value = parts[1]
		} else {
			mylog.Check(jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value))

			// Not valid JSON-- just save the string as-is.
		}

		tr.Set(s.context().InstanceName(), key, value)
	}

	return nil
}

func setInterfaceAttribute(context *hookstate.Context, staticAttrs map[string]interface{}, dynamicAttrs map[string]interface{}, key string, value interface{}) error {
	data := mylog.Check2(json.Marshal(value))

	raw := json.RawMessage(data)

	subkeys := mylog.Check2(config.ParseKey(key))

	// We're called from setInterfaceSetting, subkeys is derived from key
	// part of key=value argument and is guaranteed to be non-empty at this
	// point.
	if len(subkeys) == 0 {
		return fmt.Errorf("internal error: unexpected empty subkeys for key %q", key)
	}
	var existing interface{}
	mylog.Check(getAttribute(context.InstanceName(), subkeys[:1], 0, staticAttrs, &existing))
	if err == nil {
		return fmt.Errorf(i18n.G("attribute %q cannot be overwritten"), key)
	}
	// we expect NoAttributeError here, any other error is unexpected (a real error)
	if !isNoAttribute(err) {
		return err
	}

	_ = mylog.Check2(config.PatchConfig(context.InstanceName(), subkeys, 0, dynamicAttrs, &raw))
	return err
}

func (s *setCommand) setInterfaceSetting(context *hookstate.Context, plugOrSlot string) error {
	// Make sure set :<plug|slot> is only supported during the execution of prepare-[plug|slot] hooks
	hookType, _ := interfaceHookType(context.HookName())
	if hookType != preparePlugHook && hookType != prepareSlotHook {
		return fmt.Errorf(i18n.G("interface attributes can only be set during the execution of prepare hooks"))
	}

	attrsTask := mylog.Check2(attributesTask(context))
	mylog.Check(

		// check if the requested plug or slot is correct for this hook.
		validatePlugOrSlot(attrsTask, hookType == preparePlugHook, plugOrSlot))

	var which string
	if hookType == preparePlugHook {
		which = "plug"
	} else {
		which = "slot"
	}

	context.Lock()
	defer context.Unlock()

	var staticAttrs, dynamicAttrs map[string]interface{}
	mylog.Check(attrsTask.Get(which+"-static", &staticAttrs))

	dynKey := which + "-dynamic"
	mylog.Check(attrsTask.Get(dynKey, &dynamicAttrs))

	for _, attrValue := range s.Positional.ConfValues {
		parts := strings.SplitN(attrValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), attrValue)
		}

		var value interface{}
		mylog.Check(jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value))
		mylog.Check(
			// Not valid JSON, save the string as-is

			setInterfaceAttribute(context, staticAttrs, dynamicAttrs, parts[0], value))

	}

	attrsTask.Set(dynKey, dynamicAttrs)
	return nil
}
