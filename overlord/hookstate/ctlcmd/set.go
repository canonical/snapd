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
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/registrystate"
)

type setCommand struct {
	baseCommand

	View bool `long:"view" description:"return registry values from the view declared in the plug"`

	Positional struct {
		PlugOrSlotSpec string   `positional-arg-name:":<plug|slot>"`
		ConfValues     []string `positional-arg-name:"key=value"`
	} `positional-args:"yes"`

	String bool `short:"s" description:"parse the value as a string"`
	Typed  bool `short:"t" description:"parse the value strictly as JSON document"`
}

var shortSetHelp = i18n.G("Set either configuration options or interface connection settings")
var longSetHelp = i18n.G(`
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

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} })
}

func (s *setCommand) Execute(args []string) error {
	if s.Positional.PlugOrSlotSpec == "" && len(s.Positional.ConfValues) == 0 {
		return fmt.Errorf(i18n.G("set which option?"))
	}

	context, err := s.ensureContext()
	if err != nil {
		return err
	}

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

	if s.View {
		opts := &ParseConfigOptions{String: s.String, Typed: s.Typed}
		requests, err := ParseConfigValues(s.Positional.ConfValues, opts)
		if err != nil {
			return fmt.Errorf(i18n.G("cannot set %s plug: %w"), s.Positional.PlugOrSlotSpec, err)
		}

		return s.setRegistryValues(context, name, requests)
	}

	return s.setInterfaceSetting(context, name)
}

func (s *setCommand) setConfigSetting(context *hookstate.Context) error {
	context.Lock()
	tr := configstate.ContextTransaction(context)
	context.Unlock()

	opts := &ParseConfigOptions{String: s.String, Typed: s.Typed}
	confValues, err := ParseConfigValues(s.Positional.ConfValues, opts)
	if err != nil {
		return err
	}

	for key, value := range confValues {
		tr.Set(s.context().InstanceName(), key, value)
	}

	return nil
}

func setInterfaceAttribute(context *hookstate.Context, staticAttrs map[string]interface{}, dynamicAttrs map[string]interface{}, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot marshal snap %q option %q: %s", context.InstanceName(), key, err)
	}
	raw := json.RawMessage(data)

	subkeys, err := config.ParseKey(key)
	if err != nil {
		return err
	}

	// We're called from setInterfaceSetting, subkeys is derived from key
	// part of key=value argument and is guaranteed to be non-empty at this
	// point.
	if len(subkeys) == 0 {
		return fmt.Errorf("internal error: unexpected empty subkeys for key %q", key)
	}
	var existing interface{}
	err = getAttribute(context.InstanceName(), subkeys[:1], 0, staticAttrs, &existing)
	if err == nil {
		return fmt.Errorf(i18n.G("attribute %q cannot be overwritten"), key)
	}
	// we expect NoAttributeError here, any other error is unexpected (a real error)
	if !isNoAttribute(err) {
		return err
	}

	_, err = config.PatchConfig(context.InstanceName(), subkeys, 0, dynamicAttrs, &raw)
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
		which = "plug"
	} else {
		which = "slot"
	}

	context.Lock()
	defer context.Unlock()

	var staticAttrs, dynamicAttrs map[string]interface{}
	if err = attrsTask.Get(which+"-static", &staticAttrs); err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get %s from appropriate task, %s"), which, err)
	}

	dynKey := which + "-dynamic"
	if err = attrsTask.Get(dynKey, &dynamicAttrs); err != nil {
		return fmt.Errorf(i18n.G("internal error: cannot get %s from appropriate task, %s"), which, err)
	}

	for _, attrValue := range s.Positional.ConfValues {
		parts := strings.SplitN(attrValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), attrValue)
		}

		var value interface{}
		if err := jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value); err != nil {
			// Not valid JSON, save the string as-is
			value = parts[1]
		}
		err = setInterfaceAttribute(context, staticAttrs, dynamicAttrs, parts[0], value)
		if err != nil {
			return fmt.Errorf(i18n.G("cannot set attribute: %v"), err)
		}
	}

	attrsTask.Set(dynKey, dynamicAttrs)
	return nil
}

func (s *setCommand) setRegistryValues(ctx *hookstate.Context, plugName string, requests map[string]interface{}) error {
	ctx.Lock()
	defer ctx.Unlock()

	view, err := getRegistryView(ctx, plugName)
	if err != nil {
		return fmt.Errorf("cannot set registry: %v", err)
	}

	tx, err := registrystate.RegistryTransaction(ctx, view.Registry())
	if err != nil {
		return err
	}

	// TODO: once we have hooks, check that we don't set values in the wrong hooks
	// (e.g., "registry-changed" hooks can only read data)

	return registrystate.SetViaViewInTx(tx, view, requests)
}

// ParseConfigOptions controls how config values should be parsed.
type ParseConfigOptions struct {
	// String is enabled when values should be stored as-is w/o parsing being parsed.
	String bool
	// Typed is enabled when values should be stored parsed as JSON. If String is
	// enabled, this value is ignored.
	Typed bool
}

// ParseConfigValues parses config values in the format of "foo=bar" or "!foo",
// optionally a strict strings or JSON values depending on passed options.
// By default, values are parsed if valid JSON and stored as-is if not.
func ParseConfigValues(confValues []string, opts *ParseConfigOptions) (map[string]interface{}, error) {
	if opts == nil {
		opts = &ParseConfigOptions{}
	}

	patchValues := make(map[string]interface{}, len(confValues))
	for _, patchValue := range confValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) == 1 && strings.HasSuffix(patchValue, "!") {
			patchValues[strings.TrimSuffix(patchValue, "!")] = nil
			continue
		}

		if len(parts) != 2 {
			return nil, fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
		}

		if opts.String {
			patchValues[parts[0]] = parts[1]
		} else {
			var value interface{}
			if err := jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value); err != nil {
				if opts.Typed {
					return nil, fmt.Errorf(i18n.G("failed to parse JSON: %w"), err)
				}

				// Not valid JSON-- just save the string as-is.
				patchValues[parts[0]] = parts[1]
			} else {
				patchValues[parts[0]] = value
			}
		}
	}

	return patchValues, nil
}
