// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"errors"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortUnsetHelp = i18n.G("Remove configuration options")
var longUnsetHelp = i18n.G(`
The unset command removes the provided configuration options as requested.

	$ snap unset snap-name name address

All configuration changes are persisted at once, and only after the
snap's configuration hook returns successfully.

Nested values may be removed via a dotted path:

	$ snap unset snap-name user.name
`)

var longConfdbUnsetHelp = i18n.G(`
If the first argument passed into unset is a confdb identifier matching the
format <account-id>/<confdb>/<view>, unset will use the confdb API. In this
case, the command removes the data stored in the provided dot-separated view
paths.
`)

type cmdUnset struct {
	waitMixin
	Positional struct {
		Snap     installedSnapName
		ConfKeys []string `required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	if err := validateConfdbFeatureFlag(); err == nil {
		longUnsetHelp += longConfdbUnsetHelp
	}

	addCommand("unset", shortUnsetHelp, longUnsetHelp, func() flags.Commander { return &cmdUnset{} }, waitDescs, []argDesc{
		{
			name: "<snap>",
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("The snap to configure (e.g. hello-world)"),
		}, {
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<conf key>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Configuration key to unset"),
		},
	})
}

func (x *cmdUnset) Execute(args []string) error {
	patchValues := make(map[string]any)
	for _, confKey := range x.Positional.ConfKeys {
		if confKey == "" {
			return errors.New(i18n.G("configuration keys cannot be empty"))
		}

		patchValues[confKey] = nil
	}

	snapName := string(x.Positional.Snap)
	var id string
	var err error

	if isConfdbViewID(snapName) {
		if err := validateConfdbFeatureFlag(); err != nil {
			return err
		}

		// first argument is a confdbViewID, use the confdb API
		confdbViewID := snapName
		if err := validateConfdbViewID(confdbViewID); err != nil {
			return err
		}

		id, err = x.client.ConfdbSetViaView(confdbViewID, patchValues)
	} else {
		id, err = x.client.SetConf(snapName, patchValues)
	}

	if err != nil {
		return err
	}

	if _, err := x.wait(id); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}
