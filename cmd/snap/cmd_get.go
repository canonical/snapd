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

package main

import (
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdGet struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap name>" description:"the snap whose config is being requested"`
		Key  string `positional-arg-name:"<key>" description:"key of interest within the configuration"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("get",
		i18n.G("Get configuration for the given snap"),
		i18n.G("Get configuration for the given snap."),
		func() flags.Commander {
			return &cmdGet{}
		})
}

func (x *cmdGet) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("too many arguments: %s", strings.Join(args, " "))
	}

	return getConfig(x.Positional.Snap, x.Positional.Key)
}

func getConfig(snapName, configKey string) error {
	cli := Client()
	config, err := cli.GetConfig(snapName, configKey)
	if err != nil {
		return err
	}

	fmt.Fprintln(Stdout, config)
	return nil
}
