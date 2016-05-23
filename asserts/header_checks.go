// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package asserts

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// common checks used when decoding/assembling assertions

func checkExists(headers map[string]string, name string) (string, error) {
	value, ok := headers[name]
	if !ok {
		return "", fmt.Errorf("%q header is mandatory", name)
	}
	return value, nil
}

func checkNotEmpty(headers map[string]string, name string) (string, error) {
	value, err := checkExists(headers, name)
	if err != nil {
		return "", err
	}
	if len(value) == 0 {
		return "", fmt.Errorf("%q header should not be empty", name)
	}
	return value, nil
}

func checkAssertType(assertType *AssertionType) error {
	if assertType == nil {
		return fmt.Errorf("internal error: assertion type cannot be nil")
	}
	// sanity check against known canonical
	sanity := typeRegistry[assertType.Name]
	switch sanity {
	case assertType:
		// fine, matches canonical
		return nil
	case nil:
		return fmt.Errorf("internal error: unknown assertion type: %q", assertType.Name)
	default:
		return fmt.Errorf("internal error: unpredefined assertion type for name %q used (unexpected address %p)", assertType.Name, assertType)
	}
}

// use 'defl' default if missing
func checkInteger(headers map[string]string, name string, defl int) (int, error) {
	valueStr, ok := headers[name]
	if !ok {
		return defl, nil
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return -1, fmt.Errorf("%q header is not an integer: %v", name, valueStr)
	}
	return value, nil
}

func checkRFC3339Date(headers map[string]string, name string) (time.Time, error) {
	dateStr, err := checkNotEmpty(headers, name)
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q header is not a RFC3339 date: %v", name, err)
	}
	return date, nil
}

func checkUint(headers map[string]string, name string, bitSize int) (uint64, error) {
	valueStr, err := checkNotEmpty(headers, name)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(valueStr, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%q header is not an unsigned integer: %v", name, valueStr)
	}
	return value, nil
}

func checkCommaSepList(headers map[string]string, name string) ([]string, error) {
	listStr, ok := headers[name]
	if !ok {
		return nil, fmt.Errorf("%q header is mandatory", name)
	}

	// XXX: we likely don't need this much white-space flexibility,
	// just supporting newline after , could be enough

	// empty lists are allowed
	listStr = strings.TrimSpace(listStr)
	if listStr == "" {
		return nil, nil
	}

	entries := strings.Split(listStr, ",")
	for i, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("empty entry in comma separated %q header: %q", name, listStr)
		}
		entries[i] = entry
	}

	return entries, nil
}
