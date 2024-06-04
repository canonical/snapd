// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2023 Canonical Ltd
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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
)

type cmdValidateSeed struct {
	Positionals struct {
		SeedYamlPath flags.Filename `positional-arg-name:"<seed-yaml-path>"`
	} `positional-args:"true" required:"true"`
}

func init() {
	cmd := addDebugCommand("validate-seed",
		"(internal) validate seed.yaml",
		"(internal) validate seed.yaml",
		func() flags.Commander {
			return &cmdValidateSeed{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdValidateSeed) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	// plug/slot sanitization is disabled (no-op) by default at the package
	// level for "snap" command, for seed package use here however we want
	// real validation.
	snap.SanitizePlugsSlots = builtin.SanitizePlugsSlots

	return seed.ValidateFromYaml(string(x.Positionals.SeedYamlPath))
}
