// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package clientutil

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

var (
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

type ModelFormatter interface {
	LongPublisher(storeAccountID string) string
	GetEscapedDash() string
}

// modelAssertJSON is a helper to write out a single assertion in json
// format that matches how it's done in api_model.go. We have stolen
// the structure from there.
type modelAssertJSON struct {
	Headers map[string]interface{} `json:"headers,omitempty"`
	Body    string                 `json:"body,omitempty"`
}

type PrintModelAssertionOptions struct {
	TermWidth int
	AbsTime   bool
	Verbose   bool
	Assertion bool
}

func fmtTime(t time.Time, abs bool) string {
	if abs {
		return t.Format(time.RFC3339)
	}
	return timeutil.Human(t)
}

func printVerboseAssertionHeaders(w *tabwriter.Writer, options PrintModelAssertionOptions, assertion asserts.Assertion) error {
	allHeadersMap := assertion.Headers()
	for _, headerName := range niceOrdering {
		invalidTypeErr := fmt.Errorf("invalid type for %q header", headerName)

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
				// system-user-authority can also appear as string
				headerString, ok := headerValue.(string)
				if ok {
					fmt.Fprintf(w, "%s:\t%s\n", headerName, headerString)
					continue
				}
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

		// timestamp needs to be formatted with fmtTime from the timeMixin
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
			fmt.Fprintf(w, "timestamp:\t%s\n", fmtTime(t, options.AbsTime))

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
			case options.TermWidth > 86:
				fmt.Fprintf(w, "device-key-sha3-384: %s\n", headerString)
			case options.TermWidth <= 86 && options.TermWidth > 66:
				fmt.Fprintln(w, "device-key-sha3-384: |")
				strutil.WordWrapPadded(w, []rune(headerString), "  ", options.TermWidth)
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
			strutil.WordWrapPadded(w, []rune(headerString), "  ", options.TermWidth)

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
	return w.Flush()
}

// PrintModelAssertionYAML will format the provided serial or model assertion based on the parameters given in
// YAML format. The output will be written to the provided io.Writer.
func PrintModelAssertionYAML(w *tabwriter.Writer, modelFormatter ModelFormatter, options PrintModelAssertionOptions, modelAssertion asserts.Model, serialAssertion *asserts.Serial) error {

	// if we got an invalid model assertion bail early
	if options.Assertion {
		_, err := w.Write(asserts.Encode(&modelAssertion))
		return err
	}

	// the rest of this function is the main flow for outputting either the
	// model or serial assertion in normal or verbose mode

	// for the `snap model` case with no options, we don't want colons, we want
	// to be like `snap version`
	separator := ":"
	if !options.Verbose {
		separator = ""
	}

	// ordering of the primary keys for model: brand, model, serial
	// ordering of primary keys for serial is brand-id, model, serial

	// output brand/brand-id
	brandIDHeader := modelAssertion.HeaderString("brand-id")
	modelHeader := modelAssertion.HeaderString("model")
	// for the serial header, if there's no serial yet, it's not an error for
	// model (and we already handled the serial error above) but need to add a
	// parenthetical about the device not being registered yet
	var serial string
	if serialAssertion == nil {
		if options.Verbose {
			// verbose and serial are yamlish, so we need to escape the dash
			serial = modelFormatter.GetEscapedDash()
		} else {
			serial = "-"
		}
		serial += " (device not registered yet)"
	} else {
		serial = serialAssertion.HeaderString("serial")
	}

	// handle brand/brand-id and model/model + display-name differently on just
	// `snap model` w/o opts
	if options.Verbose {
		fmt.Fprintf(w, "brand-id:\t%s\n", brandIDHeader)
		fmt.Fprintf(w, "model:\t%s\n", modelHeader)
	} else {
		publisher := modelFormatter.LongPublisher(brandIDHeader)

		// use the longPublisher helper to format the brand store account
		// like we do in `snap info`
		fmt.Fprintf(w, "brand%s\t%s\n", separator, publisher)

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

	// return if verbose is not requested
	if !options.Verbose {
		return w.Flush()
	}
	return printVerboseAssertionHeaders(w, options, &modelAssertion)
}

// PrintModelAssertionYAML will format the provided serial or model assertion based on the parameters given in
// YAML format. The output will be written to the provided io.Writer.
func PrintSerialAssertionYAML(w *tabwriter.Writer, modelFormatter ModelFormatter, options PrintModelAssertionOptions, serialAssertion asserts.Serial) error {

	// if we got an invalid model assertion bail early
	if options.Assertion {
		_, err := w.Write(asserts.Encode(&serialAssertion))
		return err
	}

	// the rest of this function is the main flow for outputting either the
	// serial assertion in normal or verbose mode

	// ordering of the primary keys for model: brand, model, serial
	// ordering of primary keys for serial is brand-id, model, serial

	// output brand/brand-id
	brandIDHeader := serialAssertion.HeaderString("brand-id")
	modelHeader := serialAssertion.HeaderString("model")
	serial := serialAssertion.HeaderString("serial")

	fmt.Fprintf(w, "brand-id:\t%s\n", brandIDHeader)
	fmt.Fprintf(w, "model:\t%s\n", modelHeader)
	fmt.Fprintf(w, "serial:\t%s\n", serial)

	// return if verbose is not requested
	if !options.Verbose {
		return w.Flush()
	}
	return printVerboseAssertionHeaders(w, options, &serialAssertion)
}

// PrintModelAssertionJSON will format the provided serial or model assertion based on the parameters given in
// JSON format. The output will be written to the provided io.Writer.
func PrintModelAssertionJSON(w *tabwriter.Writer, modelFormatter ModelFormatter, options PrintModelAssertionOptions, modelAssertion asserts.Model, serialAssertion *asserts.Serial) error {

	serializeJSON := func(v interface{}) error {
		marshalled, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(marshalled)
		w.Flush()
		return err
	}

	if options.Assertion {
		modelJSON := modelAssertJSON{}
		modelJSON.Headers = modelAssertion.Headers()
		modelJSON.Body = string(modelAssertion.Body())
		return serializeJSON(modelJSON)
	}

	// ordering of the primary keys for model: brand, model, serial
	// ordering of primary keys for serial is brand-id, model, serial

	// output brand/brand-id
	modelData := make(map[string]interface{})
	modelData["brand-id"] = modelAssertion.HeaderString("brand-id")
	modelData["model"] = modelAssertion.HeaderString("model")

	// for the serial header, if there's no serial yet, it's not an error for
	// model (and we already handled the serial error above) but need to add a
	// parenthetical about the device not being registered yet
	var serial string
	if serialAssertion == nil {
		// For JSON verbose is always true, so we need to escape the dash
		serial = modelFormatter.GetEscapedDash()
		serial += " (device not registered yet)"
	} else {
		serial = serialAssertion.HeaderString("serial")
	}

	// only output the grade if it is non-empty, either it is not in the model
	// assertion for all non-uc20 model assertions, or it is non-empty and
	// required for uc20 model assertions
	grade := modelAssertion.HeaderString("grade")
	if grade != "" {
		modelData["grade"] = grade
	}

	storageSafety := modelAssertion.HeaderString("storage-safety")
	if storageSafety != "" {
		modelData["storage-safety"] = storageSafety
	}

	// serial is same for all variants
	modelData["serial"] = serial
	allHeadersMap := modelAssertion.Headers()

	// always print extra information for JSON
	for _, headerName := range niceOrdering {
		invalidTypeErr := fmt.Errorf("invalid type for %q header", headerName)

		headerValue, ok := allHeadersMap[headerName]
		// make sure the header is in the map
		if !ok {
			continue
		}

		// switch on which header it is to handle some special cases
		switch headerName {
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
			modelData["timestamp"] = fmtTime(t, options.AbsTime)

		case "snaps":
			// also flush the writer before continuing so the previous keys
			// don't try to align with this key
			snapsHeader, ok := headerValue.([]interface{})
			if !ok {
				return invalidTypeErr
			}
			if len(snapsHeader) == 0 {
				// unexpected why this is an empty list, but just ignore for
				// now
				continue
			}
			snapsData := make(map[string]interface{})
			for _, sn := range snapsHeader {
				snMap, ok := sn.(map[string]interface{})
				if !ok {
					return invalidTypeErr
				}
				// iterate over all keys in the map in a stable, visually
				// appealing ordering
				// first do snap name, which will always be present since we
				// parsed a valid assertion
				snapsData["name"] = snMap["name"].(string)

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
						snapsData[snKey] = snStrValue
					}
				}

				// finally handle "modes" which is a list
				modes, ok := snMap["modes"]
				if !ok {
					continue
				}
				snapsData["modes"] = modes
			}
			modelData["snaps"] = snapsData

		// long base64 key we can rewrap safely
		case "device-key":
			headerString, ok := headerValue.(string)
			if !ok {
				return invalidTypeErr
			}
			// remove the newlines from the string
			modelData["device-key"] = strings.Replace(headerString, "\n", "", -1)

		// the default is all the rest of short scalar values, which all
		// should be strings
		default:
			modelData[headerName] = headerValue
		}
	}

	return serializeJSON(modelData)
}
