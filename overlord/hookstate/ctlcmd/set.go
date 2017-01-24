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

type setCommand struct {
	baseCommand

	Positional struct {
		PlugOrSlotSpec string   `positional-arg-name:"<snap>:<plug|slot>" required:"yes"`
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
`)

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} }, nil, nil)
}

func (s *setCommand) Execute(args []string) error {
	context := s.context()
	if context == nil {
		return fmt.Errorf("cannot set without a context")
	}

	var snapAndPlugOrSlot snap.SnapAndName
	// treat PlugOrSlotSpec argument as key=value if it contans '=' or doesn't contain ':' - this is to support
	// valus such as "device-service.url=192.168.0.1:5555" and error out on invalid key=value if only "key" is given.
	if strings.Contains(s.Positional.PlugOrSlotSpec, "=") || !strings.Contains(s.Positional.PlugOrSlotSpec, ":") {
		s.Positional.ConfValues = append([]string{s.Positional.PlugOrSlotSpec}, s.Positional.ConfValues[0:]...)
		s.Positional.PlugOrSlotSpec = ""
	} else {
		snapAndPlugOrSlot.UnmarshalFlag(s.Positional.PlugOrSlotSpec)
	}

	if snapAndPlugOrSlot.Snap != "" && snapAndPlugOrSlot.Snap != s.context().SnapName() {
		return fmt.Errorf(i18n.G("cannot set interface attribute of other snap: %q"), snapAndPlugOrSlot.Snap)
	}
	if snapAndPlugOrSlot.Name != "" {
		// Make sure set :<plug|slot> is only supported during the execution of prepare-[plug|slot] hooks
		if !(strings.HasPrefix(context.HookName(), "prepare-slot-") ||
			strings.HasPrefix(context.HookName(), "prepare-plug-")) {
			return fmt.Errorf(i18n.G("interface attributes can only be set during the execution of prepare-plug- and prepare-slot- hooks"))
		}
		return s.handleSetInterfaceAttributes(context)
	}

	context.Lock()
	transaction := configstate.ContextTransaction(context)
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

		transaction.Set(s.context().SnapName(), key, value)
	}

	return nil
}

func (s *setCommand) handleSetInterfaceAttributes(context *hookstate.Context) error {
	context.Lock()
	defer context.Unlock()

	var attributes map[string]map[string]interface{}
	if err := context.Get("attributes", &attributes); err != nil {
		if err == state.ErrNoState {
			return fmt.Errorf(i18n.G("no attributes found"))
		}
		return err
	}

	var attrs map[string]interface{}
	var ok bool
	if attrs, ok = attributes[context.SnapName()]; !ok {
		// this should never happen unless there is an inconsistency in hook task setup.
		return fmt.Errorf(i18n.G("missing attributes for snap %s"), context.SnapName())
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
		attrs[parts[0]] = value
	}

	attributes[context.SnapName()] = attrs
	s.context().Set("attributes", attributes)
	return nil
}
