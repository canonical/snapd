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

var shortSetHelp = i18n.G("Changes configuration options")
var longSetHelp = i18n.G(`
The set command changes the provided configuration options as requested.

    $ snap set snap-name username=frank password=$PASSWORD

All configuration changes are persisted at once, and only after the
snap's configuration hook returns successfully.

Nested values may be modified via a dotted path:

    $ snap set author.name=frank
`)

type cmdSet struct {
	Positional struct {
		Snap       installedSnapName
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

	return configure(string(x.Positional.Snap), patchValues)
}

func configure(snapName string, patchValues map[string]interface{}) error {
	cli := Client()
	id, err := cli.SetConf(snapName, patchValues)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
