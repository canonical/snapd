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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdKnown struct {
	KnownOptions struct {
		AssertTypeName string   `positional-arg-name:"<assertion type>" description:"assertion type name" required:"true"`
		HeaderFilters  []string `positional-arg-name:"<header filters>" description:"header=value" required:"0"`
	} `positional-args:"true" required:"true"`
}

var shortKnownHelp = i18n.G("Shows known assertions of the provided type")
var longKnownHelp = i18n.G(`
The known command shows known assertions of the provided type.
If header=value pairs are provided after the assertion type, the assertions
shown must also have the specified headers matching the provided values.
`)

func init() {
	addCommand("known", shortKnownHelp, longKnownHelp, func() flags.Commander {
		return &cmdKnown{}
	})
}

var nl = []byte{'\n'}

func (x *cmdKnown) Execute(args []string) error {
	// TODO: share this kind of parsing once it's clearer how often is used in snap
	headers := map[string]string{}
	for _, headerFilter := range x.KnownOptions.HeaderFilters {
		parts := strings.SplitN(headerFilter, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header filter: %q (want key=value)", headerFilter)
		}
		headers[parts[0]] = parts[1]
	}

	assertions, err := Client().Known(x.KnownOptions.AssertTypeName, headers)
	if err != nil {
		return err
	}

	enc := asserts.NewEncoder(Stdout)
	for _, a := range assertions {
		enc.Encode(a)
	}

	return nil
}
