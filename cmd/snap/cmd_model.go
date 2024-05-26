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
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
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
	errNoSerial           = errors.New(i18n.G("device not registered yet (no serial assertion found)"))
	errNoVerboseAssertion = errors.New(i18n.G("cannot use --verbose with --assertion"))
)

// cmdModelFormatter implements the interface required by clientutil.Print*
// functions, as it formats the output it requires some extra information from
// environment its called from.
type cmdModelFormatter struct {
	client *client.Client
	esc    *escapes
}

func (mf cmdModelFormatter) GetEscapedDash() string {
	return mf.esc.dash
}

func (mf cmdModelFormatter) LongPublisher(storeAccountID string) string {
	storeAccount := mylog.Check2(mf.client.StoreAccount(storeAccountID))

	// use the longPublisher helper to format the brand store account
	// like we do in `snap info`
	return longPublisher(mf.esc, storeAccount)
}

type cmdModel struct {
	clientMixin
	timeMixin
	colorMixin

	Serial    bool `long:"serial"`
	Verbose   bool `long:"verbose"`
	Assertion bool `long:"assertion"`
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
	if serialErr != nil && !client.IsAssertionNotFoundError(serialErr) {
		return serialErr
	}

	if x.Assertion {
		// if we are using the serial assertion and we specifically didn't find the
		// serial assertion, bail with specific error
		if x.Serial && client.IsAssertionNotFoundError(serialErr) {
			return errNoMainAssertion
		}
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	w := tabWriter()

	if x.Serial && client.IsAssertionNotFoundError(serialErr) {
		// for serial assertion, the primary keys are output (model and
		// brand-id), but if we didn't find the serial assertion then we still
		// output the brand-id and model from the model assertion, but also
		// return a devNotReady error
		fmt.Fprintf(w, "brand-id:\t%s\n", modelAssertion.HeaderString("brand-id"))
		fmt.Fprintf(w, "model:\t%s\n", modelAssertion.HeaderString("model"))
		w.Flush()
		return errNoSerial
	}

	modelFormatter := cmdModelFormatter{
		esc:    x.getEscapes(),
		client: x.client,
	}
	opts := clientutil.PrintModelAssertionOptions{
		TermWidth: termWidth,
		AbsTime:   x.AbsTime,
		Verbose:   x.Verbose,
		Assertion: x.Assertion,
	}
	if x.Serial {
		mylog.Check(clientutil.PrintSerialAssertionYAML(w, *serialAssertion, modelFormatter, opts))
	} else {
		mylog.Check(clientutil.PrintModelAssertion(w, *modelAssertion, serialAssertion, modelFormatter, opts))
	}
	return w.Flush()
}
