// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdReboot struct {
	clientMixin
	Positional struct {
		Label string
	} `positional-args:"true" required:"true"`

	RunMode     bool `long:"run"`
	InstallMode bool `long:"install"`
	RecoverMode bool `long:"recover"`
}

var shortRebootHelp = i18n.G("Reboot into selected system and mode")
var longRebootHelp = i18n.G(`
The reboot command reboots the system into a particular mode of the selected
recovery system.
`)

func init() {
	addCommand("reboot", shortRebootHelp, longRebootHelp, func() flags.Commander {
		return &cmdReboot{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"run": i18n.G("Boot into run mode"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"install": i18n.G("Boot into install mode"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"recover": i18n.G("Boot into recover mode"),
	}, []argDesc{
		{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<label>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("The recovery system label"),
		},
	})
}

func (x *cmdReboot) modeFromCommandline() (*client.SystemAction, error) {
	var action client.SystemAction
	for _, arg := range []struct {
		enabled bool
		mode    string
	}{
		{x.RunMode, "run"},
		{x.RecoverMode, "recover"},
		{x.InstallMode, "install"},
	} {
		if !arg.enabled {
			continue
		}
		if action.Mode != "" {
			return nil, fmt.Errorf("Please specify a single mode")
		}
		action.Mode = arg.mode
	}
	// XXX: should we have a default here?
	if action.Mode == "" {
		return nil, fmt.Errorf("Please specify a mode, see --help")
	}

	return &action, nil
}

func (x *cmdReboot) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	action, err := x.modeFromCommandline()
	if err != nil {
		return err
	}

	if err := x.client.DoSystemAction(x.Positional.Label, action); err != nil {
		return fmt.Errorf("cannot reboot into system %q: %v", x.Positional.Label, err)
	}

	fmt.Fprintf(Stdout, "Reboot into %q with mode %q scheduled.\n", x.Positional.Label, action.Mode)
	return nil
}
