// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package ubootenv

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Env contains the data of the uboot environment stored in a given format
type Env interface {
	Get(string) string
	Set(string, string)
	Save() error

	// mostly for tests or debugging
	String() string
	Size() int
}

// OpenFlags instructs open how to alter its behavior.
type OpenFlags int

// TODO: support "binary" format too?

// Format is the uboot environment format
type Format int

const (
	// OpenBestEffort instructs OpenWithFlags to skip malformed data without
	// returning an error.
	OpenBestEffort OpenFlags = 1 << iota

	// OpenIgnoreComments instructs OpenWithFlags to read lines/entries that
	// start with a "#" character, the same way mkenvimage does with input files
	OpenIgnoreComments

	// NativeFormat is the "uboot native" format which has a CRC, delimits vars
	// with '\0' and has padding on it
	NativeFormat Format = iota
	// TextFormat is the simple text format for vars
	TextFormat
)

// Create will initialize a uboot env file with the given options. For
// TextFormat the size parameter is ignored.
func Create(fname string, format Format, size int) (Env, error) {
	switch format {
	case NativeFormat:
		return createNative(fname, size)
	case TextFormat:
		return createText(fname)
	}

	return nil, fmt.Errorf("unsupported format %v", format)
}

// Open opens a existing uboot env file
func Open(fname string, fmt Format) (Env, error) {
	return OpenWithFlags(fname, fmt, OpenFlags(0))
}

// OpenWithFlags opens a existing uboot env file, passing additional flags.
func OpenWithFlags(fname string, format Format, flags OpenFlags) (Env, error) {
	switch format {
	case NativeFormat:
		return openNativeFormat(fname, flags)
	case TextFormat:
		return openTextFormat(fname, flags)
	}

	return nil, fmt.Errorf("unsupported format %v", format)
}

// common helper methods shared between the implementations

// parseData parses env binary data, with each env var delimited by the specified
// delimited, and then in the form "<key>=<value>"
func parseData(data []byte, lineDelimiter byte, flags OpenFlags) (map[string]string, error) {
	out := make(map[string]string)

	for _, envStr := range bytes.Split(data, []byte{lineDelimiter}) {
		if len(envStr) == 0 || envStr[0] == 0 || envStr[0] == 255 {
			continue
		}
		entry := string(envStr)
		if flags&OpenIgnoreComments == OpenIgnoreComments && entry[0] == '#' {
			continue
		}
		l := strings.SplitN(entry, "=", 2)
		if len(l) != 2 || l[0] == "" {
			if flags&OpenBestEffort == OpenBestEffort {
				continue
			}
			return nil, fmt.Errorf("cannot parse line %q as key=value pair", envStr)
		}
		key := l[0]
		value := l[1]
		out[key] = value
	}

	return out, nil
}

// iterEnv calls the passed function f with key, value for environment
// vars. The order is guaranteed (unlike just iterating over the map)
func iterEnv(data map[string]string, f func(key, value string)) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k == "" {
			panic("iterEnv iterating over a empty key")
		}

		f(k, data[k])
	}
}

func writeData(w io.Writer, data map[string]string, delimiter byte) {
	iterEnv(data, func(key, value string) {
		w.Write([]byte(fmt.Sprintf("%s=%s", key, value)))
		w.Write([]byte{delimiter})
	})
}
