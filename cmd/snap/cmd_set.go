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
	"github.com/snapcore/snapd/jsonutil"
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

var longAspectSetHelp = i18n.G(`
If the first argument passed into set is an aspect identifier matching the
format <account-id>/<bundle>/<aspect>, set will use the aspects configuration
API. In this case, the command sets the values as provided for the dot-separated
aspect paths.
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
	if err := validateAspectFeatureFlag(); err == nil {
		longSetHelp += longAspectSetHelp
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
		return fmt.Errorf(i18n.G("cannot use -t and -s together"))
	}

	patchValues := make(map[string]interface{})
	for _, patchValue := range x.Positional.ConfValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) == 1 && strings.HasSuffix(patchValue, "!") {
			patchValues[strings.TrimSuffix(patchValue, "!")] = nil
			continue
		}
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
		}

		if x.String {
			patchValues[parts[0]] = parts[1]
		} else {
			var value interface{}
			if err := jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value); err != nil {
				if x.Typed {
					return fmt.Errorf("failed to parse JSON: %w", err)
				}

				// Not valid JSON-- just save the string as-is.
				patchValues[parts[0]] = parts[1]
			} else {
				patchValues[parts[0]] = value
			}
		}
	}

	snapName := string(x.Positional.Snap)

	var chgID string
	var err error
	if isAspectID(snapName) {
		if err := validateAspectFeatureFlag(); err != nil {
			return err
		}

		// first argument is an aspectID, use the aspects API
		aspectID := snapName
		if err := validateAspectID(aspectID); err != nil {
			return err
		}

		chgID, err = x.client.AspectSet(aspectID, patchValues)
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

func isAspectID(s string) bool {
	return len(strings.Split(s, "/")) == 3
}

func validateAspectID(id string) error {
	parts := strings.Split(id, "/")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf(i18n.G("aspect identifier must conform to format: <account-id>/<bundle>/<aspect>"))
		}
	}

	return nil
}
