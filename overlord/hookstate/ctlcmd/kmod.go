// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil/kmod"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

var (
	shortKmodHelp = i18n.G("Load or unload kernel modules")
	longKmodHelp  = i18n.G(`
The kmod command handles loading and unloading of kernel modules.`)
)

func init() {
	addCommand("kmod", shortKmodHelp, longKmodHelp, func() command {
		cmd := &kmodCommand{}
		cmd.InsertCmd.kmodBaseCommand = cmd
		cmd.RemoveCmd.kmodBaseCommand = cmd
		return cmd
	})
}

type kmodCommand struct {
	baseCommand
	InsertCmd KModInsertCmd `command:"insert" description:"load a kernel module"`
	RemoveCmd KModRemoveCmd `command:"remove" description:"unload a kernel module"`
}

type kmodSubcommand struct {
	kmodBaseCommand *kmodCommand
	snapInfo        *snap.Info
}

type KModInsertCmd struct {
	kmodSubcommand
	Positional struct {
		Module  string   `positional-arg-name:"<module>" required:"yes" description:"name of the kernel module to be loaded"`
		Options []string `positional-arg-name:"<options>" description:"kernel module options"`
	} `positional-args:"yes" required:"yes"`
}

func (k *KModInsertCmd) Execute([]string) error {
	context, err := k.kmodBaseCommand.ensureContext()
	if err != nil {
		return err
	}

	attributes, err := k.findConnection(context, k.Positional.Module, k.Positional.Options)
	if err != nil {
		return fmt.Errorf("cannot load module %q: %v", k.Positional.Module, err)
	}

	if len(attributes) == 0 {
		snapName := context.InstanceName()
		return fmt.Errorf("snap %q lacks permissions to load the module %q", snapName, k.Positional.Module)
	}

	if err := kmod.LoadModule(k.Positional.Module, k.Positional.Options); err != nil {
		return fmt.Errorf("cannot load module %q: %v", k.Positional.Module, err)
	}

	return nil
}

type KModRemoveCmd struct {
	kmodSubcommand
	Positional struct {
		Module string `positional-arg-name:"<module>" required:"yes" description:"name of the kernel module to be unloaded"`
	} `positional-args:"yes" required:"yes"`
}

func (k *KModRemoveCmd) Execute([]string) error {
	context, err := k.kmodBaseCommand.ensureContext()
	if err != nil {
		return err
	}

	attributes, err := k.findConnection(context, k.Positional.Module, []string{})
	if err != nil {
		return fmt.Errorf("cannot unload module %q: %v", k.Positional.Module, err)
	}

	if len(attributes) == 0 {
		snapName := context.InstanceName()
		return fmt.Errorf("snap %q lacks permissions to unload the module %q", snapName, k.Positional.Module)
	}

	if err := kmod.UnloadModule(k.Positional.Module); err != nil {
		return fmt.Errorf("cannot unload module %q: %v", k.Positional.Module, err)
	}

	return nil
}

// matchConnection checks whether the given kmod connection attributes give
// the snap permission to execute the kmod command
func (k *kmodSubcommand) matchConnection(attributes map[string]interface{}, moduleName string, moduleOptions []string) bool {
	if attributes["load"].(string) != "dynamic" {
		return false
	}

	if moduleName != attributes["name"].(string) {
		return false
	}

	if len(moduleOptions) > 0 {
		// snapctl can be invoked with options only if the "options" attribute
		// on the plug is set to "*"
		optionsAttr, ok := attributes["options"]
		if !ok || optionsAttr.(string) != "*" {
			return false
		}
	}

	return true
}

// findConnections walks through the established connections to find one which
// is compatible with a kmod operation on the given moduleName and
// moduleOptions. If found, it returns the connection's attributes.
func (k *kmodSubcommand) findConnection(context *hookstate.Context, moduleName string, moduleOptions []string) (attributes map[string]interface{}, err error) {
	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	k.snapInfo, err = snapstate.CurrentInfo(st, snapName)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot get snap info: %s", err)
	}

	for connId, connState := range conns {
		if connState.Interface != "kernel-module-load" {
			continue
		}

		if !connState.Active() {
			continue
		}

		connRef, err := interfaces.ParseConnRef(connId)
		if err != nil {
			return nil, err
		}

		if connRef.PlugRef.Snap != snapName {
			continue
		}

		modules, ok := connState.StaticPlugAttrs["modules"].([]interface{})
		if !ok {
			continue
		}

		for _, moduleAttributes := range modules {
			attributes := moduleAttributes.(map[string]interface{})
			if k.matchConnection(attributes, moduleName, moduleOptions) {
				return attributes, nil
			}
		}
	}
	return nil, nil
}

func (m *kmodCommand) Execute([]string) error {
	// This is needed in order to implement the interface, but it's never
	// called.
	return nil
}
