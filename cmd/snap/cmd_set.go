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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortSetHelp = i18n.G("Set snap configuration")
var longSetHelp = i18n.G(`
The set command sets configuration parameters for the given snap. This command
accepts a number of key=value pairs of parameters.`)

type cmdSet struct {
	Positional struct {
		Snap       string
		ConfValues []string `required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() flags.Commander { return &cmdSet{} }, nil, []argDesc{
		{
			name: "<snap>",
			desc: i18n.G("The snap to configure (e.g. hello-world)"),
		}, {
			name: i18n.G("<conf value>"),
			desc: i18n.G("Configuration value (key=value)"),
		},
	})
}

func (x *cmdSet) Execute(args []string) error {
	patchValues := make(map[string]interface{})
	for _, patchValue := range x.Positional.ConfValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
		}
		var value interface{}
		err := json.Unmarshal([]byte(parts[1]), &value)
		if err == nil {
			patchValues[parts[0]] = value
		} else {
			// Not valid JSON-- just save the string as-is.
			patchValues[parts[0]] = parts[1]
		}
	}

	return applyConfig(x.Positional.Snap, patchValues)
}

func applyConfig(snapName string, patchValues map[string]interface{}) error {
	cli := Client()
	id, err := cli.SetConf(snapName, patchValues)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
