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
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
)

type setCommand struct {
	baseCommand

	Positional struct {
		PlugOrSlotSpec string   `positional-arg-name:":<plug|slot>"`
		ConfValues     []string `positional-arg-name:"key=value"`
	} `positional-args:"yes"`
}

var shortSetHelp = i18n.G("Changes configuration options")
var longSetHelp = i18n.G(`
The set command changes the provided configuration options as requested.

    $ snapctl set username=frank password=$PASSWORD

All configuration changes are persisted at once, and only after the hook
returns successfully.

Nested values may be modified via a dotted path:

    $ snapctl set author.name=frank

Plug and slot attributes may be set in the respective prepare and connect hooks by
naming the respective plug or slot:

    $ snapctl set :myplug path=/dev/ttyS0
`)

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} })
}

func (s *setCommand) Execute(args []string) error {
	if s.Positional.PlugOrSlotSpec == "" && len(s.Positional.ConfValues) == 0 {
		return fmt.Errorf(i18n.G("set which option?"))
	}

	context := s.context()
	if context == nil {
		return fmt.Errorf("cannot set without a context")
	}

	// treat PlugOrSlotSpec argument as key=value if it contans '=' or doesn't contain ':' - this is to support
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
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), patchValue)
		}
		key := parts[0]
		var value interface{}
		err := json.Unmarshal([]byte(parts[1]), &value)
		if err != nil {
			// Not valid JSON-- just save the string as-is.
			value = parts[1]
		}

		tr.Set(s.context().SnapName(), key, value)
	}

	return nil
}

func setInterfaceAttribute(context *hookstate.Context, attributes map[string]interface{}, protectedAttrs map[string]interface{}, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot marshal snap %q option %q: %s", context.SnapName(), key, err)
	}
	raw := json.RawMessage(data)

	subkeys, err := config.ParseKey(key)
	if err != nil {
		return err
	}

	var existing interface{}
	err = config.GetFromChange(context.SnapName(), subkeys, 0, protectedAttrs, &existing)
	if err == nil {
		return fmt.Errorf(i18n.G("attribute %q cannot be overwritten"), key)
	}

	_, err = config.PatchConfig(context.SnapName(), subkeys, 0, attributes, &raw)
	return err
}

func (s *setCommand) setInterfaceSetting(context *hookstate.Context, plugOrSlot string) error {
	// Make sure set :<plug|slot> is only supported during the execution of prepare-[plug|slot] hooks
	hookType, _ := interfaceHookType(context.HookName())
	if hookType != preparePlugHook && hookType != prepareSlotHook {
		return fmt.Errorf(i18n.G("interface attributes can only be set during the execution of prepare hooks"))
	}

	attrsTask, err := attributesTask(context)
	if err != nil {
		return err
	}

	// check if the requested plug or slot is correct for this hook.
	if err := validatePlugOrSlot(attrsTask, hookType == preparePlugHook, plugOrSlot); err != nil {
		return err
	}

	var which string
	if hookType == preparePlugHook {
		which = "plug-attrs"
	} else {
		which = "slot-attrs"
	}

	context.Lock()
	defer context.Unlock()

	attributes := make(map[string]interface{})
	if err := attrsTask.Get(which, &attributes); err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get %s from appropriate task"), which)
	}

	// If this is the first time 'set' is called, store and mark initial attributes as protected.
	protectedAttrs := context.Cached("protected-attrs")
	if protectedAttrs == nil {
		protectedAttrs, err = copyAttributes(attributes)
		if err != nil {
			panic(fmt.Sprintf("internal error: cannot copy attributes %q", err))
		}
		context.Cache("protected-attrs", protectedAttrs)
	}

	for _, attrValue := range s.Positional.ConfValues {
		parts := strings.SplitN(attrValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), attrValue)
		}

		var value interface{}
		err := json.Unmarshal([]byte(parts[1]), &value)
		if err != nil {
			// Not valid JSON, save the string as-is
			value = parts[1]
		}
		err = setInterfaceAttribute(context, attributes, protectedAttrs.(map[string]interface{}), parts[0], value)
		if err != nil {
			return fmt.Errorf(i18n.G("cannot set attribute: %v"), err)
		}
	}

	attrsTask.Set(which, attributes)
	return nil
}

func copyAttributes(value map[string]interface{}) (map[string]interface{}, error) {
	cpy, err := copyRecursive(value)
	if err != nil {
		return nil, err
	}
	return cpy.(map[string]interface{}), err
}

func copyRecursive(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return v, nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, el := range v {
			tmp, err := copyRecursive(el)
			if err != nil {
				return nil, err
			}
			arr[i] = tmp
		}
		return arr, nil
	case map[string]interface{}:
		mp := make(map[string]interface{}, len(v))
		for key, item := range v {
			tmp, err := copyRecursive(item)
			if err != nil {
				return nil, err
			}
			mp[key] = tmp
		}
		return mp, nil
	default:
		return nil, fmt.Errorf("unsupported attribute type '%T', value '%v'", value, value)
	}
}
