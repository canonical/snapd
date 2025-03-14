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
	"errors"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
)

var shortSetHelp = i18n.G("Change configuration options")
var longSetHelp = i18n.G(`
The set command changes the provided configuration options as requested.

    $ snap set snap-name username=frank password=$PASSWORD

All configuration changes are persisted at once, and only after the
snap's configuration hook returns successfully.

Nested values may be modified via a dotted path:

    $ snap set snap-name author.name=frank

Configuration option may be unset with exclamation mark:
    $ snap set snap-name author!
`)

var longConfdbSetHelp = i18n.G(`
If the first argument passed into set is a confdb identifier matching the
format <account-id>/<confdb>/<view>, set will use the confdb API. In this
case, the command sets the values as provided for the dot-separated view paths.
`)

type cmdSet struct {
	waitMixin
	Positional struct {
		Snap       installedSnapName
		ConfValues []string `required:"1"`
	} `positional-args:"yes" required:"yes"`

	Typed  bool `short:"t"`
	String bool `short:"s"`
}

func init() {
	if err := validateConfdbFeatureFlag(); err == nil {
		longSetHelp += longConfdbSetHelp
	}

	addCommand("set", shortSetHelp, longSetHelp, func() flags.Commander { return &cmdSet{} },
		waitDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"t": i18n.G("Parse the value strictly as JSON document"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"s": i18n.G("Parse the value as a string"),
		}), []argDesc{
			{
				name: "<snap>",
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The snap to configure (e.g. hello-world)"),
			}, {
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<conf value>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("Set (key=value) or unset (key!) configuration value"),
			},
		})
}

func (x *cmdSet) Execute([]string) error {
	if x.String && x.Typed {
		return errors.New(i18n.G("cannot use -t and -s together"))
	}

	opts := &clientutil.ParseConfigOptions{String: x.String, Typed: x.Typed}
	patchValues, _, err := clientutil.ParseConfigValues(x.Positional.ConfValues, opts)
	if err != nil {
		return err
	}

	snapName := string(x.Positional.Snap)
	var chgID string
	if isConfdbViewID(snapName) {
		if err := validateConfdbFeatureFlag(); err != nil {
			return err
		}

		// first argument is a confdbViewID, use the confdb API
		confdbViewID := snapName
		if err := validateConfdbViewID(confdbViewID); err != nil {
			return err
		}

		chgID, err = x.client.ConfdbSetViaView(confdbViewID, patchValues)
	} else {
		chgID, err = x.client.SetConf(snapName, patchValues)
	}

	if err != nil {
		return err
	}

	if _, err := x.wait(chgID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}

func isConfdbViewID(s string) bool {
	return len(strings.Split(s, "/")) == 3
}

func validateConfdbViewID(id string) error {
	parts := strings.Split(id, "/")
	for _, part := range parts {
		if part == "" {
			return errors.New(i18n.G("confdb-schema view id must conform to format: <account-id>/<confdb-schema>/<view>"))
		}
	}

	return nil
}
