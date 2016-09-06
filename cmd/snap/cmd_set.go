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

type cmdSet struct {
	Positional struct {
		Snap         string   `positional-arg-name:"<snap name>" description:"the snap to configure (e.g. hello-world)"`
		ConfigValues []string `positional-arg-name:"<config value>" description:"configuration value (key=value)" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("set",
		i18n.G("Set snap configuration"),
		i18n.G(`Set configuration for the given snap. This command accepts a
			number of key=value pairs of configuration parameters`),
		func() flags.Commander {
			return &cmdSet{}
		})
}

func (x *cmdSet) Execute(args []string) error {
	configValues := make(map[string]string)
	for _, configValue := range x.Positional.ConfigValues {
		parts := strings.SplitN(configValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid config: %q (want key=value)", configValue)
		}
		configValues[parts[0]] = parts[1]
	}

	return applyConfig(x.Positional.Snap, configValues)
}

func applyConfig(snapName string, configValues map[string]string) error {
	cli := Client()
	config := map[string]interface{}{"config": configValues}
	id, err := cli.SetConf(snapName, config)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
