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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
)

var (
	shortModelHelp = i18n.G("Get the active model for this device")
	longModelHelp  = i18n.G(`
The model command returns the active model assertion information for this
device.

By default, only the essential model identification information is
included in the output, but this can be expanded to include all of an
assertion's non-meta headers.

The verbose output is presented in a structured, yaml-like format.

Similarly, the active serial assertion can be used for the output instead of the
model assertion.
`)

	errNoMainAssertion    = errors.New(i18n.G("device not ready yet (no assertions found)"))
	errNoVerboseAssertion = errors.New(i18n.G("cannot use --verbose with --assertion"))
	errNoSerial           = errors.New(i18n.G("device not registered yet (no serial assertion found)"))

	// this list is a "nice" "human" "readable" "ordering" of headers to print
	// off, sorted in lexical order with meta headers and primary key headers
	// removed, and big nasty keys such as device-key-sha3-384 and
	// device-key at the bottom
	// it also contains both serial and model assertion headers, but we
	// follow the same code path for both assertion types and some of the
	// headers are shared between the two, so it still works out correctly
	niceOrdering = [...]string{
		"architecture",
		"base",
		"classic",
		"display-name",
		"gadget",
		"kernel",
		"revision",
		"store",
		"system-user-authority",
		"timestamp",
		"required-snaps", // for uc16 and uc18 models
		"snaps",          // for uc20 models
		"device-key-sha3-384",
		"device-key",
	}
)

type cmdModel struct {
	clientMixin
	timeMixin
	colorMixin

	Serial    bool `long:"serial"`
	Verbose   bool `long:"verbose"`
	Assertion bool `long:"assertion"`
}

type cmdModelFormatter struct {
	storeAccount *snap.StoreAccount
	esc          *escapes
}

func (mf cmdModelFormatter) GetEscapedDash() string {
	return mf.esc.dash
}

func (mf cmdModelFormatter) GetPublisher() string {
	return longPublisher(mf.esc, mf.storeAccount)
}

func init() {
	addCommand("model",
		shortModelHelp,
		longModelHelp,
		func() flags.Commander {
			return &cmdModel{}
		}, colorDescs.also(timeDescs).also(map[string]string{
			"assertion": i18n.G("Print the raw assertion."),
			"verbose":   i18n.G("Print all specific assertion fields."),
			"serial": i18n.G(
				"Print the serial assertion instead of the model assertion."),
		}),
		[]argDesc{},
	)
}

func (x *cmdModel) Execute(args []string) error {
	if x.Verbose && x.Assertion {
		// can't do a verbose mode for the assertion
		return errNoVerboseAssertion
	}

	serialAssertion, serialErr := x.client.CurrentSerialAssertion()
	modelAssertion, modelErr := x.client.CurrentModelAssertion()

	// if we didn't get a model assertion bail early
	if modelErr != nil {
		if client.IsAssertionNotFoundError(modelErr) {
			// device is not registered yet - use specific error message
			return errNoMainAssertion
		}
		return modelErr
	}

	// if the serial assertion error is anything other than not found, also
	// bail early
	// the serial assertion not being found may not be fatal
	if serialErr != nil {
		if !client.IsAssertionNotFoundError(serialErr) {
			return serialErr
		}
		serialAssertion = nil
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	w := tabWriter()
	format := clientutil.MODELWRITER_YAML_FORMAT
	modelFormatter := cmdModelFormatter{
		esc: x.getEscapes(),
	}

	// for the model command (not --serial) we want to show a publisher
	// style display of "brand" instead of just "brand-id"
	if !x.Serial && !x.Verbose {
		brandIDHeader := modelAssertion.HeaderString("brand-id")
		storeAccount, err := x.client.StoreAccount(brandIDHeader)
		if err != nil {
			return err
		}
		modelFormatter.storeAccount = storeAccount
	}

	if !x.Serial && !x.Verbose {
		format = clientutil.MODELWRITER_RAW_FORMAT
	}

	err := clientutil.PrintModelAssertation(
		w, format, modelFormatter,
		termWidth, x.Serial, x.AbsTime, x.Verbose, x.Assertion,
		modelAssertion, serialAssertion)
	if err != nil {
		return err
	}
	return w.Flush()
}
