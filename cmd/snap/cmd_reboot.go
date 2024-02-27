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

	"github.com/snapcore/snapd/i18n"
)

type cmdReboot struct {
	clientMixin
	Positional struct {
		Label string
	} `positional-args:"true"`

	RunMode          bool `long:"run"`
	InstallMode      bool `long:"install"`
	RecoverMode      bool `long:"recover"`
	FactoryResetMode bool `long:"factory-reset"`
}

var shortRebootHelp = i18n.G("Reboot into selected system and mode")
var longRebootHelp = i18n.G(`
The reboot command reboots the system into a particular mode of the selected
recovery system.

When called without a system label and without a mode it will just
trigger a regular reboot.

When called without a system label, the current system will be used for the
"run" and "install" modes. The default recovery system will be used for the
"recover" and "factory-reset" modes.

Note that the "run" mode is only available for the current system.
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
		// TRANSLATORS: This should not start with a lowercase letter.
		"factory-reset": i18n.G("Boot into factory-reset mode"),
	}, []argDesc{
		{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<label>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("The recovery system label"),
		},
	})
}

func (x *cmdReboot) modeFromCommandline() (string, error) {
	var mode string

	for _, arg := range []struct {
		enabled bool
		mode    string
	}{
		{x.RunMode, "run"},
		{x.RecoverMode, "recover"},
		{x.InstallMode, "install"},
		{x.FactoryResetMode, "factory-reset"},
	} {
		if !arg.enabled {
			continue
		}
		if mode != "" {
			return "", fmt.Errorf(i18n.G("Please specify a single mode"))
		}
		mode = arg.mode
	}

	return mode, nil
}

func (x *cmdReboot) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	mode, err := x.modeFromCommandline()
	if err != nil {
		return err
	}

	if err := x.client.RebootToSystem(x.Positional.Label, mode); err != nil {
		return err
	}

	switch {
	case x.Positional.Label != "" && mode != "":
		fmt.Fprintf(Stdout, i18n.G("Reboot into %q %q mode.\n"), x.Positional.Label, mode)
	case x.Positional.Label != "":
		fmt.Fprintf(Stdout, i18n.G("Reboot into %q.\n"), x.Positional.Label)
	case mode != "":
		fmt.Fprintf(Stdout, i18n.G("Reboot into %q mode.\n"), mode)
	default:
		fmt.Fprintf(Stdout, i18n.G("Reboot\n"))
	}

	return nil
}
