// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/snappy"
)

// FIXME: remove once the native security via interfaces is in place
type cmdPolicygen struct {
	RegenerateAll bool `long:"regenerate-all"`
	Compare       bool `long:"compare"`
	Force         bool `short:"f" long:"force" description:"Force policy generation."`
	Positional    struct {
		SnapYaml string `positional-arg-name:"snap.yaml path" description:"The path to the snap.yaml used to generate the apparmor policy."`
	} `positional-args:"yes"`
}

func init() {
	cmd := addCommand("policygen",
		i18n.G("Generate the apparmor policy"),
		i18n.G("Generate the apparmor policy"),
		func() flags.Commander {
			return &cmdPolicygen{}
		})
	cmd.hidden = true
}

func (x *cmdPolicygen) Execute(args []string) error {
	return withMutexAndRetry(x.doPolicygen)
}

func (x *cmdPolicygen) doPolicygen() error {
	if x.RegenerateAll {
		return snappy.RegenerateAllPolicy(x.Force)
	}

	fn := x.Positional.SnapYaml
	if fn == "" {
		return fmt.Errorf(i18n.G("must supply path to snap.yaml"))
	}
	if _, err := os.Stat(fn); err != nil {
		return fmt.Errorf("policygen: no such file: %s", fn)
	}

	if x.Compare {
		return snappy.CompareGeneratePolicyFromFile(fn)
	}

	return snappy.GeneratePolicyFromFile(fn, x.Force)
}
