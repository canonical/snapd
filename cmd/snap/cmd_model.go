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
	"fmt"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortModelHelp = i18n.G("Get the active model for this device")
	longModelHelp  = i18n.G(`
The model command returns the active model assertion information for this
device.

By default, only the essential model identification information is
included in the output, but this can be expanded to include all of an
assertion's headers non-meta headers.

Similarly, the active serial assertion can be used for the output instead of the
model assertion.
`)

	invalidTypeMessage = i18n.G("invalid type for %q header")

	// this list is a "nice" "human" "readable" "ordering" of headers to print
	// off, sorted in lexographical order with meta headers and primary key
	// headers removed, and big nasty keys such as device-key-sha3-384 and
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
		"timestamp",
		"required-snaps",
		"device-key-sha3-384",
		"device-key",
	}
)

type cmdModel struct {
	waitMixin
	timeMixin
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
		}, timeDescs.also(waitDescs).also(map[string]string{
			"assertion": i18n.G("Print the raw assertion."),
			"verbose":   i18n.G("Print all specific assertion fields."),
			"serial": i18n.G(
				"Print the serial assertion instead of the model assertion."),
		}),
		[]argDesc{},
	)
}

func (x *cmdModel) Execute(args []string) error {
	var assertion asserts.Assertion
	var err error
	if x.Serial {
		assertion, err = x.client.CurrentSerialAssertion()
	} else {
		assertion, err = x.client.CurrentModelAssertion()
	}

	if err != nil {
		if client.IsAssertionNotFoundError(err) {
			// device is not registered yet - use specific error message
			err = fmt.Errorf(i18n.G("device not ready - no assertion found"))
		}
		return err
	}

	// --assertion means output the raw assertion
	if x.Assertion {
		_, err = Stdout.Write(asserts.Encode(assertion))
		return err
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	w := tabWriter()

	// output the primary keys first in their canonical order
	for _, headerName := range assertion.Type().PrimaryKey {
		if headerName == "series" {
			// series can be obtained from "snap version"
			// and given it is stuck at 16 is mainly confusing
			continue
		}
		fmt.Fprintf(w, "%s:\t%s\n", headerName, assertion.HeaderString(headerName))
	}

	// --verbose means output all of the fields
	if x.Verbose {
		allHeadersMap := assertion.Headers()

		for _, headerName := range niceOrdering {
			invalidTypeErr := fmt.Errorf(invalidTypeMessage, headerName)

			headerValue, ok := allHeadersMap[headerName]
			// make sure the header is in the map
			if !ok {
				continue
			}

			// switch on which header it is to handle some special cases
			switch headerName {
			// list of scalars
			case "required-snaps":
				headerIfaceList, ok := headerValue.([]interface{})
				if !ok {
					return invalidTypeErr
				}
				if len(headerIfaceList) == 0 {
					continue
				} else {
					fmt.Fprintf(w, "%s:\t\n", headerName)
					for _, elem := range headerIfaceList {
						headerStringElem, ok := elem.(string)
						if !ok {
							return invalidTypeErr
						}
						// note we don't wrap these, since for now this is
						// specifically just required-snaps and so all of these
						// will be snap names which are required to be short
						fmt.Fprintf(w, "  - %s\n", headerStringElem)
					}
				}

			//timestamp needs to be formatted with fmtTime from the timeMixin
			case "timestamp":
				timestamp, ok := allHeadersMap[headerName].(string)
				if !ok {
					return invalidTypeErr
				}

				// parse the time string as RFC3339, which is what the format is
				// always in for assertions
				t, err := time.Parse(time.RFC3339, timestamp)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "timestamp:\t%s\n", x.fmtTime(t))

			// long string key we don't want to rewrap but can safely handle
			// on "reasonable" width terminals
			case "device-key-sha3-384":
				// also flush the writer before continuing so the previous keys
				// don't try to align with this key
				w.Flush()
				headerString, ok := headerValue.(string)
				if !ok {
					return invalidTypeErr
				}

				switch {
				case termWidth > 86:
					fmt.Fprintf(w, "device-key-sha3-384: %s\n", headerString)
				case termWidth <= 86 && termWidth > 66:
					fmt.Fprintln(w, "device-key-sha3-384: |")
					wrapLine(w, []rune(headerString), "  ", termWidth)
				}

			// long base64 key we can rewrap safely
			case "device-key":
				headerString, ok := headerValue.(string)
				if !ok {
					return invalidTypeErr
				}
				// the string value here has newlines inserted as part of the
				// raw assertion, but base64 doesn't care about whitespace, so
				// it's safe to split by newlines and re-wrap to make it
				// prettier
				headerString = strings.Join(
					strings.Split(headerString, "\n"),
					"")
				fmt.Fprintln(w, "device-key: |")
				wrapLine(w, []rune(headerString), "  ", termWidth)

			// the default is all the rest of short scalar values, which all
			// should be strings
			default:
				headerString, ok := headerValue.(string)
				if !ok {
					return invalidTypeErr
				}
				fmt.Fprintf(w, "%s:\t%s\n", headerName, headerString)
			}
		}
	}

	return w.Flush()
}
