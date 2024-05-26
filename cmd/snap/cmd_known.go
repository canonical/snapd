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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

type cmdKnown struct {
	clientMixin
	KnownOptions struct {
		// XXX: how to get a list of assert types for completion?
		AssertTypeName assertTypeName `required:"true"`
		HeaderFilters  []string       `required:"0"`
	} `positional-args:"true" required:"true"`

	Remote bool `long:"remote"`
	Direct bool `long:"direct"`
}

var (
	shortKnownHelp = i18n.G("Show known assertions of the provided type")
	longKnownHelp  = i18n.G(`
The known command shows known assertions of the provided type.
If header=value pairs are provided after the assertion type, the assertions
shown must also have the specified headers matching the provided values.
`)
)

func init() {
	addCommand("known", shortKnownHelp, longKnownHelp, func() flags.Commander {
		return &cmdKnown{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"remote": i18n.G("Query the store for the assertion, via snapd if possible"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"direct": i18n.G("Query the store for the assertion, without attempting to go via snapd"),
	}, []argDesc{
		{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<assertion type>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Assertion type name"),
		}, {
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<header filter>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Constrain listing to those matching header=value"),
		},
	})
}

var storeNew = store.New

func downloadAssertion(typeName string, headers map[string]string) ([]asserts.Assertion, error) {
	var user *auth.UserState

	// FIXME: set auth context
	var storeCtx store.DeviceAndAuthContext

	at := asserts.Type(typeName)
	if at == nil {
		return nil, fmt.Errorf("cannot find assertion type %q", typeName)
	}
	primaryKeys := mylog.Check2(asserts.PrimaryKeyFromHeaders(at, headers))

	sto := storeNew(nil, storeCtx)
	as := mylog.Check2(sto.Assertion(at, primaryKeys, user))

	return []asserts.Assertion{as}, nil
}

func (x *cmdKnown) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// TODO: share this kind of parsing once it's clearer how often is used in snap
	headers := map[string]string{}
	for _, headerFilter := range x.KnownOptions.HeaderFilters {
		parts := strings.SplitN(headerFilter, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid header filter: %q (want key=value)"), headerFilter)
		}
		headers[parts[0]] = parts[1]
	}

	var assertions []asserts.Assertion

	switch {
	case x.Remote && !x.Direct:
		// --remote will query snapd
		assertions = mylog.Check2(x.client.Known(string(x.KnownOptions.AssertTypeName), headers, &client.KnownOptions{Remote: true}))
		// if snapd is unavailable automatically fallback
		var connErr client.ConnectionError
		if xerrors.As(err, &connErr) {
			assertions = mylog.Check2(downloadAssertion(string(x.KnownOptions.AssertTypeName), headers))
		}
	case x.Direct:
		// --direct implies remote
		assertions = mylog.Check2(downloadAssertion(string(x.KnownOptions.AssertTypeName), headers))
	default:
		// default is to look only local
		assertions = mylog.Check2(x.client.Known(string(x.KnownOptions.AssertTypeName), headers, nil))
	}

	enc := asserts.NewEncoder(Stdout)
	for _, a := range assertions {
		enc.Encode(a)
	}

	return nil
}
