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
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
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

	invalidTypeMessage    = i18n.G("invalid type for %q header")
	errNoMainAssertion    = errors.New(i18n.G("device not ready yet (no assertions found)"))
	errNoSerial           = errors.New(i18n.G("device not registered yet (no serial assertion found)"))
	errNoVerboseAssertion = errors.New(i18n.G("cannot use --verbose with --assertion"))

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

	var mainAssertion asserts.Assertion
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

	if x.Serial {
		mainAssertion = serialAssertion
	} else {
		mainAssertion = modelAssertion
	}

	if x.Assertion {
		// if we are using the serial assertion and we specifically didn't find the
		// serial assertion, bail with specific error
		if x.Serial && client.IsAssertionNotFoundError(serialErr) {
			return errNoMainAssertion
		}

		_, err := Stdout.Write(asserts.Encode(mainAssertion))
		return err
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	esc := x.getEscapes()

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

	// the rest of this function is the main flow for outputting either the
	// model or serial assertion in normal or verbose mode

	// for the `snap model` case with no options, we don't want colons, we want
	// to be like `snap version`
	separator := ":"
	if !x.Verbose && !x.Serial {
		separator = ""
	}

	// ordering of the primary keys for model: brand, model, serial
	// ordering of primary keys for serial is brand-id, model, serial

	// output brand/brand-id
	brandIDHeader := mainAssertion.HeaderString("brand-id")
	modelHeader := mainAssertion.HeaderString("model")
	// for the serial header, if there's no serial yet, it's not an error for
	// model (and we already handled the serial error above) but need to add a
	// parenthetical about the device not being registered yet
	var serial string
	if client.IsAssertionNotFoundError(serialErr) {
		if x.Verbose || x.Serial {
			// verbose and serial are yamlish, so we need to escape the dash
			serial = esc.dash
		} else {
			serial = "-"
		}
		serial += " (device not registered yet)"
	} else {
		serial = serialAssertion.HeaderString("serial")
	}

	// handle brand/brand-id and model/model + display-name differently on just
	// `snap model` w/o opts
	if x.Serial || x.Verbose {
		fmt.Fprintf(w, "brand-id:\t%s\n", brandIDHeader)
		fmt.Fprintf(w, "model:\t%s\n", modelHeader)
	} else {
		// for the model command (not --serial) we want to show a publisher
		// style display of "brand" instead of just "brand-id"
		storeAccount, err := x.client.StoreAccount(brandIDHeader)
		if err != nil {
			return err
		}
		// use the longPublisher helper to format the brand store account
		// like we do in `snap info`
		fmt.Fprintf(w, "brand%s\t%s\n", separator, longPublisher(x.getEscapes(), storeAccount))

		// for model, if there's a display-name, we show that first with the
		// real model in parenthesis
		if displayName := modelAssertion.HeaderString("display-name"); displayName != "" {
			modelHeader = fmt.Sprintf("%s (%s)", displayName, modelHeader)
		}
		fmt.Fprintf(w, "model%s\t%s\n", separator, modelHeader)
	}

	// only output the grade if it is non-empty, either it is not in the model
	// assertion for all non-uc20 model assertions, or it is non-empty and
	// required for uc20 model assertions
	grade := modelAssertion.HeaderString("grade")
	if grade != "" {
		fmt.Fprintf(w, "grade%s\t%s\n", separator, grade)
	}

	storageSafety := modelAssertion.HeaderString("storage-safety")
	if storageSafety != "" {
		fmt.Fprintf(w, "storage-safety%s\t%s\n", separator, storageSafety)
	}

	// serial is same for all variants
	fmt.Fprintf(w, "serial%s\t%s\n", separator, serial)

	// --verbose means output more information
	if x.Verbose {
		allHeadersMap := mainAssertion.Headers()

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
			case "required-snaps", "system-user-authority":
				headerIfaceList, ok := headerValue.([]interface{})
				if !ok {
					return invalidTypeErr
				}
				if len(headerIfaceList) == 0 {
					continue
				}
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

			//timestamp needs to be formatted with fmtTime from the timeMixin
			case "timestamp":
				timestamp, ok := headerValue.(string)
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
					strutil.WordWrapPadded(w, []rune(headerString), "  ", termWidth)
				}
			case "snaps":
				// also flush the writer before continuing so the previous keys
				// don't try to align with this key
				w.Flush()
				snapsHeader, ok := headerValue.([]interface{})
				if !ok {
					return invalidTypeErr
				}
				if len(snapsHeader) == 0 {
					// unexpected why this is an empty list, but just ignore for
					// now
					continue
				}
				fmt.Fprintf(w, "snaps:\n")
				for _, sn := range snapsHeader {
					snMap, ok := sn.(map[string]interface{})
					if !ok {
						return invalidTypeErr
					}
					// iterate over all keys in the map in a stable, visually
					// appealing ordering
					// first do snap name, which will always be present since we
					// parsed a valid assertion
					name := snMap["name"].(string)
					fmt.Fprintf(w, "  - name:\t%s\n", name)

					// the rest of these may be absent, but they are all still
					// simple strings
					for _, snKey := range []string{"id", "type", "default-channel", "presence"} {
						snValue, ok := snMap[snKey]
						if !ok {
							continue
						}
						snStrValue, ok := snValue.(string)
						if !ok {
							return invalidTypeErr
						}
						if snStrValue != "" {
							fmt.Fprintf(w, "    %s:\t%s\n", snKey, snStrValue)
						}
					}

					// finally handle "modes" which is a list
					modes, ok := snMap["modes"]
					if !ok {
						continue
					}
					modesSlice, ok := modes.([]interface{})
					if !ok {
						return invalidTypeErr
					}
					if len(modesSlice) == 0 {
						continue
					}

					modeStrSlice := make([]string, 0, len(modesSlice))
					for _, mode := range modesSlice {
						modeStr, ok := mode.(string)
						if !ok {
							return invalidTypeErr
						}
						modeStrSlice = append(modeStrSlice, modeStr)
					}
					modesSliceYamlStr := "[" + strings.Join(modeStrSlice, ", ") + "]"
					fmt.Fprintf(w, "    modes:\t%s\n", modesSliceYamlStr)
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
				strutil.WordWrapPadded(w, []rune(headerString), "  ", termWidth)

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
