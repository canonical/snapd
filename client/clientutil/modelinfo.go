// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeutil"
)

var (
	invalidTypeMessage = i18n.G("invalid type for %q header")
	errNoMainAssertion = errors.New(i18n.G("device not ready yet (no assertions found)"))
	errNoSerial        = errors.New(i18n.G("device not registered yet (no serial assertion found)"))

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

type OutputFormat int

type ModelFormatter interface {
	GetPublisher() string
	GetEscapedDash() string
}

const (
	MODELWRITER_RAW_FORMAT OutputFormat = iota
	MODELWRITER_YAML_FORMAT
	MODELWRITER_JSON_FORMAT
)

type modelAssertJSON struct {
	Headers map[string]interface{} `json:"headers,omitempty"`
	Body    string                 `json:"body,omitempty"`
}

type modelWriterState struct {
	indent       int
	firstElement bool
	inArray      bool
	inlineArray  bool
	arrayMembers []string
}

type modelWriter struct {
	w            *tabwriter.Writer
	format       OutputFormat
	currentState modelWriterState
	states       []modelWriterState
}

// runesTrimRightSpace returns text, with any trailing whitespace dropped.
func runesTrimRightSpace(text []rune) []rune {
	j := len(text)
	for j > 0 && unicode.IsSpace(text[j-1]) {
		j--
	}
	return text[:j]
}

// wrapLine wraps a line, assumed to be part of a block-style yaml
// string, to fit into termWidth, preserving the line's indent, and
// writes it out prepending padding to each line.
func wrapLine(out io.Writer, text []rune, pad string, termWidth int) error {
	// discard any trailing whitespace
	text = runesTrimRightSpace(text)
	// establish the indent of the whole block
	idx := 0
	for idx < len(text) && unicode.IsSpace(text[idx]) {
		idx++
	}
	indent := pad + string(text[:idx])
	text = text[idx:]
	if len(indent) > termWidth/2 {
		// If indent is too big there's not enough space for the actual
		// text, in the pathological case the indent can even be bigger
		// than the terminal which leads to lp:1828425.
		// Rather than let that happen, give up.
		indent = pad + "  "
	}
	return strutil.WordWrap(out, text, indent, indent, termWidth)
}

func (w *modelWriter) pushState() {
	w.states = append(w.states, w.currentState)
	w.currentState = modelWriterState{
		indent:       w.currentState.indent,
		firstElement: true,
		inArray:      false,
		inlineArray:  false,
		arrayMembers: nil,
	}
}

func (w *modelWriter) popState() {
	w.currentState = w.states[len(w.states)-1]
	w.states = w.states[:len(w.states)-1]
}

func (w *modelWriter) increaseIndent() {
	w.currentState.indent += 2
}

func (w *modelWriter) indent() string {
	return strings.Repeat(" ", w.currentState.indent)
}

func (w *modelWriter) writeSeperator() {
	if w.currentState.firstElement {
		w.currentState.firstElement = false
		return
	}

	if w.format == MODELWRITER_JSON_FORMAT {
		fmt.Fprint(w.w, ",\n")
	} else {
		fmt.Fprint(w.w, "\n")
	}
}

func (w *modelWriter) StartObject(name string) {
	w.writeSeperator()

	// store this for later use with json arrays
	inArray := w.currentState.inArray

	if w.format == MODELWRITER_JSON_FORMAT {
		if len(name) > 0 {
			// named object, are we in an array?
			if w.currentState.inArray {
				fmt.Fprint(w.w, w.indent(), "{\n")
			} else {
				fmt.Fprint(w.w, w.indent(), `"`, name, `": {`, "\n")
			}
		} else {
			// anonymous object
			fmt.Fprint(w.w, w.indent(), `{`, "\n")
		}
	} else if w.format == MODELWRITER_YAML_FORMAT {
		if w.currentState.inArray {
			fmt.Fprintf(w.w, "%s- name:\t%s\n", w.indent(), name)
		} else if len(name) > 0 {
			fmt.Fprintf(w.w, "%s%s:\n", w.indent(), name)
		}
	}

	w.pushState()
	// the only time we do not increase indentation for yaml
	// is for the root object, which is called like this
	// startObject("")
	if w.format != MODELWRITER_YAML_FORMAT || len(name) != 0 {
		w.increaseIndent()
	}

	// For objects in arrays in json we need to write the name of
	// the object seperately. We need to do this post state is pushed
	// and after the indentation has increased
	if w.format == MODELWRITER_JSON_FORMAT && inArray {
		w.WriteStringPair("name", name)
	}
}

func (w *modelWriter) EndObject() {
	w.popState()
	if w.format == MODELWRITER_JSON_FORMAT {
		fmt.Fprintf(w.w, "\n%s%s", w.indent(), "}")
	}

	// root element? add a newline
	if len(w.states) == 0 {
		fmt.Fprint(w.w, "\n")
	}
}

// startArray marks that any following members will be part of an array
// instead of the outer object. The array can be inlined, which means that
// it will fit it into one single line. This is useful for yaml to format arrays
// as [1,2,3] instead of
// - 1
// - 2
// - 3
func (w *modelWriter) StartArray(name string, inline bool) {
	w.writeSeperator()
	if w.format == MODELWRITER_JSON_FORMAT {
		fmt.Fprintf(w.w, "%s%q: [\n", w.indent(), name)
	} else {
		if inline {
			fmt.Fprintf(w.w, "%s%s:\t[", w.indent(), name)
		} else {
			fmt.Fprintf(w.w, "%s%s:\t\n", w.indent(), name)
		}
	}

	w.pushState()
	w.increaseIndent()
	w.currentState.inArray = true
	w.currentState.inlineArray = inline
}

func (w *modelWriter) EndArray() {
	if w.format == MODELWRITER_JSON_FORMAT {
		for i, member := range w.currentState.arrayMembers {
			fmt.Fprintf(w.w, "%s%q", w.indent(), member)
			if i < len(w.currentState.arrayMembers)-1 {
				fmt.Fprint(w.w, ",\n")
			}
		}
		fmt.Fprintf(w.w, "\n%s]", strings.Repeat(" ", w.currentState.indent-2))
	} else if w.format == MODELWRITER_YAML_FORMAT {
		if w.currentState.inlineArray {
			fmt.Fprintf(w.w, "%s]", strings.Join(w.currentState.arrayMembers, ", "))
		} else {
			for i, member := range w.currentState.arrayMembers {
				fmt.Fprintf(w.w, "%s- %s", w.indent(), member)
				if i < len(w.currentState.arrayMembers)-1 {
					fmt.Fprint(w.w, "\n")
				}
			}
		}
	}
	w.popState()
}

func (w *modelWriter) WriteStringPair(name, value string) {
	if w.currentState.inArray {
		// support for this we could do, but we wont as it's not required at this moment
		return
	}

	w.writeSeperator()
	if w.format == MODELWRITER_JSON_FORMAT {
		fmt.Fprintf(w.w, "%s%q: %q", w.indent(), name, value)
	} else if w.format == MODELWRITER_YAML_FORMAT {
		fmt.Fprintf(w.w, "%s%s:\t%s", w.indent(), name, value)
	} else {
		fmt.Fprintf(w.w, "%s%s\t%s", w.indent(), name, value)
	}
}

func (w *modelWriter) WriteWrappedStringPair(name, value string, lineWidth int) error {
	w.writeSeperator()
	if w.format == MODELWRITER_JSON_FORMAT {
		fmt.Fprintf(w.w, "%s%q: %s", w.indent(), name, value)
	} else if w.format == MODELWRITER_YAML_FORMAT {
		fmt.Fprintf(w.w, "%s%s: |\n", w.indent(), name)
		if err := wrapLine(w.w, []rune(value), "  "+w.indent(), lineWidth); err != nil {
			return err
		}
	}
	return nil
}

func (w *modelWriter) WriteStringValue(value string) {
	// this is only ever called for array members currently, if it was to be used
	// for other cases, they need to be implemented here.
	if w.currentState.inArray {
		w.currentState.arrayMembers = append(w.currentState.arrayMembers, value)
	}
}

func newModelWriter(w *tabwriter.Writer, format OutputFormat) *modelWriter {
	return &modelWriter{
		w:      w,
		format: format,
		currentState: modelWriterState{
			firstElement: true,
		},
	}
}

func fmtTime(t time.Time, abs bool) string {
	if abs {
		return t.Format(time.RFC3339)
	}
	return timeutil.Human(t)
}

func PrintModelAssertation(w *tabwriter.Writer, format OutputFormat, modelFormatter ModelFormatter, termWidth int, useSerial, absTime, verbose, assertation bool, modelAssertion *asserts.Model, serialAssertion *asserts.Serial) error {
	var mainAssertion asserts.Assertion

	// if we got an invalid model assertion bail early
	if modelAssertion == nil {
		return errNoMainAssertion
	}

	if useSerial {
		mainAssertion = serialAssertion
	} else {
		mainAssertion = modelAssertion
	}

	if assertation {
		// if we are using the serial assertion and we specifically didn't find the
		// serial assertion, bail with specific error
		if useSerial && serialAssertion == nil {
			return errNoMainAssertion
		}

		var data []byte
		if format == MODELWRITER_JSON_FORMAT {
			modelJSON := modelAssertJSON{}
			modelJSON.Headers = mainAssertion.Headers()
			modelJSON.Body = string(mainAssertion.Body())
			marshalled, err := json.MarshalIndent(modelJSON, "", "  ")
			if err != nil {
				return err
			}
			data = marshalled
		} else {
			data = asserts.Encode(mainAssertion)
		}
		_, err := w.Write(data)
		return err
	}

	mw := newModelWriter(w, format)

	if useSerial && serialAssertion == nil {
		// for serial assertion, the primary keys are output (model and
		// brand-id), but if we didn't find the serial assertion then we still
		// output the brand-id and model from the model assertion, but also
		// return a devNotReady error
		mw.StartObject("")
		mw.WriteStringPair("brand-id", modelAssertion.HeaderString("brand-id"))
		mw.WriteStringPair("model", modelAssertion.HeaderString("model"))
		mw.EndObject()
		w.Flush()
		return errNoSerial
	}

	// the rest of this function is the main flow for outputting either the
	// model or serial assertion in normal or verbose mode
	mw.StartObject("")

	// ordering of the primary keys for model: brand, model, serial
	// ordering of primary keys for serial is brand-id, model, serial

	// output brand/brand-id
	brandIDHeader := mainAssertion.HeaderString("brand-id")
	modelHeader := mainAssertion.HeaderString("model")
	// for the serial header, if there's no serial yet, it's not an error for
	// model (and we already handled the serial error above) but need to add a
	// parenthetical about the device not being registered yet
	var serial string
	if serialAssertion == nil {
		if verbose || useSerial {
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
	if verbose || useSerial {
		mw.WriteStringPair("brand-id", brandIDHeader)
		mw.WriteStringPair("model", modelHeader)
	} else {
		publisher := modelFormatter.GetPublisher()
		mw.WriteStringPair("brand", publisher)

		// for model, if there's a display-name, we show that first with the
		// real model in parenthesis
		if displayName := modelAssertion.HeaderString("display-name"); displayName != "" {
			modelHeader = fmt.Sprintf("%s (%s)", displayName, modelHeader)
		}
		mw.WriteStringPair("model", modelHeader)
	}

	// only output the grade if it is non-empty, either it is not in the model
	// assertion for all non-uc20 model assertions, or it is non-empty and
	// required for uc20 model assertions
	grade := modelAssertion.HeaderString("grade")
	if grade != "" {
		mw.WriteStringPair("grade", grade)
	}

	storageSafety := modelAssertion.HeaderString("storage-safety")
	if storageSafety != "" {
		mw.WriteStringPair("storage-safety", storageSafety)
	}

	// serial is same for all variants
	mw.WriteStringPair("serial", serial)

	// verbose means output more information
	if verbose {
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

				mw.StartArray(headerName, false)
				for _, elem := range headerIfaceList {
					headerStringElem, ok := elem.(string)
					if !ok {
						return invalidTypeErr
					}
					// note we don't wrap these, since for now this is
					// specifically just required-snaps and so all of these
					// will be snap names which are required to be short
					mw.WriteStringValue(headerStringElem)
				}
				mw.EndArray()

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
				mw.WriteStringPair("timestamp", fmtTime(t, absTime))

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
					mw.WriteStringPair("device-key-sha3-384", headerString)
				case termWidth <= 86 && termWidth > 66:
					if err := mw.WriteWrappedStringPair("device-key-sha3-384", headerString, termWidth); err != nil {
						return err
					}
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
				mw.StartArray("snaps", false)
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
					mw.StartObject(name)

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
							mw.WriteStringPair(snKey, snStrValue)
						}
					}

					// finally handle "modes" which is a list
					modes, ok := snMap["modes"]
					if !ok {
						mw.EndObject()
						continue
					}
					modesSlice, ok := modes.([]interface{})
					if !ok {
						return invalidTypeErr
					}
					if len(modesSlice) == 0 {
						mw.EndObject()
						continue
					}

					mw.StartArray("modes", true)
					for _, mode := range modesSlice {
						modeStr, ok := mode.(string)
						if !ok {
							return invalidTypeErr
						}
						mw.WriteStringValue(modeStr)
					}
					mw.EndArray()
					mw.EndObject()
				}
				mw.EndArray()

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
				if err := mw.WriteWrappedStringPair("device-key", headerString, termWidth); err != nil {
					return err
				}

			// the default is all the rest of short scalar values, which all
			// should be strings
			default:
				headerString, ok := headerValue.(string)
				if !ok {
					return invalidTypeErr
				}
				mw.WriteStringPair(headerName, headerString)
			}
		}
	}
	mw.EndObject()
	return w.Flush()
}
