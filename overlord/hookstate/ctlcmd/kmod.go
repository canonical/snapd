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
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil/kmod"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
)

var (
	shortKmodHelp = i18n.G("Load or unload kernel modules")
	longKmodHelp  = i18n.G(`
The kmod command handles loading and unloading of kernel modules.`)

	kmodLoadModule   = kmod.LoadModule
	kmodUnloadModule = kmod.UnloadModule
)

func init() {
	addCommand("kmod", shortKmodHelp, longKmodHelp, func() command {
		cmd := &kmodCommand{}
		cmd.InsertCmd.kmod = cmd
		cmd.RemoveCmd.kmod = cmd
		return cmd
	})
}

func (m *kmodCommand) Execute([]string) error {
	// This is needed in order to implement the interface, but it's never
	// called.
	return nil
}

type kmodCommand struct {
	baseCommand
	InsertCmd KModInsertCmd `command:"insert" description:"load a kernel module"`
	RemoveCmd KModRemoveCmd `command:"remove" description:"unload a kernel module"`
}

type KModInsertCmd struct {
	Positional struct {
		Module  string   `positional-arg-name:"<module>" required:"yes" description:"kernel module name"`
		Options []string `positional-arg-name:"<options>" description:"kernel module options"`
	} `positional-args:"yes" required:"yes"`
	kmod *kmodCommand
}

func (k *KModInsertCmd) Execute([]string) error {
	context := mylog.Check2(k.kmod.ensureContext())
	mylog.Check(kmodCheckConnection(context, k.Positional.Module, k.Positional.Options))
	mylog.Check(kmodLoadModule(k.Positional.Module, k.Positional.Options))

	return nil
}

type KModRemoveCmd struct {
	Positional struct {
		Module string `positional-arg-name:"<module>" required:"yes" description:"kernel module name"`
	} `positional-args:"yes" required:"yes"`
	kmod *kmodCommand
}

func (k *KModRemoveCmd) Execute([]string) error {
	context := mylog.Check2(k.kmod.ensureContext())
	mylog.Check(kmodCheckConnection(context, k.Positional.Module, []string{}))
	mylog.Check(kmodUnloadModule(k.Positional.Module))

	return nil
}

// kmodMatchConnection checks whether the given kmod connection attributes give
// the snap permission to execute the kmod command
func kmodMatchConnection(attributes map[string]interface{}, moduleName string, moduleOptions []string) bool {
	load, found := attributes["load"]
	if !found || load.(string) != "dynamic" {
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

// kmodCheckConnection walks through the established connections to find one which
// is compatible with a kmod operation on the given moduleName and
// moduleOptions. Returns an error if not found.
var kmodCheckConnection = func(context *hookstate.Context, moduleName string, moduleOptions []string) (err error) {
	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	conns := mylog.Check2(ifacestate.ConnectionStates(st))

	for connId, connState := range conns {
		if connState.Interface != "kernel-module-load" {
			continue
		}

		if !connState.Active() {
			continue
		}

		connRef := mylog.Check2(interfaces.ParseConnRef(connId))

		if connRef.PlugRef.Snap != snapName {
			continue
		}

		modules, ok := connState.StaticPlugAttrs["modules"].([]interface{})
		if !ok {
			continue
		}

		for _, moduleAttributes := range modules {
			attributes := moduleAttributes.(map[string]interface{})
			if kmodMatchConnection(attributes, moduleName, moduleOptions) {
				return nil
			}
		}
	}
	return errors.New("required interface not connected")
}
