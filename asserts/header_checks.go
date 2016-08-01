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
	"crypto"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// common checks used when decoding/assembling assertions

func checkExistsString(headers map[string]interface{}, name string) (string, error) {
	value, ok := headers[name]
	if !ok {
		return "", fmt.Errorf("%q header is mandatory", name)
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%q header must be a string", name)
	}
	return s, nil
}

func checkNotEmptyString(headers map[string]interface{}, name string) (string, error) {
	s, err := checkExistsString(headers, name)
	if err != nil {
		return "", err
	}
	if len(s) == 0 {
		return "", fmt.Errorf("%q header should not be empty", name)
	}
	return s, nil
}

func checkPrimaryKey(headers map[string]interface{}, primKey string) (string, error) {
	value, err := checkNotEmptyString(headers, primKey)
	if err != nil {
		return "", err
	}
	if strings.Contains(value, "/") {
		return "", fmt.Errorf("%q primary key header cannot contain '/'", primKey)
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
func checkIntWithDefault(headers map[string]interface{}, name string, defl int) (int, error) {
	value, ok := headers[name]
	if !ok {
		return defl, nil
	}
	s, ok := value.(string)
	if !ok {
		return -1, fmt.Errorf("%q header is not an integer: %v", name, value)
	}
	m, err := strconv.Atoi(s)
	if err != nil {
		return -1, fmt.Errorf("%q header is not an integer: %v", name, s)
	}
	return m, nil
}

func checkInt(headers map[string]interface{}, name string) (int, error) {
	valueStr, err := checkNotEmptyString(headers, name)
	if err != nil {
		return -1, err
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return -1, fmt.Errorf("%q header is not an integer: %v", name, valueStr)
	}
	return value, nil
}

func checkRFC3339Date(headers map[string]interface{}, name string) (time.Time, error) {
	dateStr, err := checkNotEmptyString(headers, name)
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q header is not a RFC3339 date: %v", name, err)
	}
	return date, nil
}

func checkUint(headers map[string]interface{}, name string, bitSize int) (uint64, error) {
	valueStr, err := checkNotEmptyString(headers, name)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(valueStr, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%q header is not an unsigned integer: %v", name, valueStr)
	}
	return value, nil
}

func checkDigest(headers map[string]interface{}, name string, h crypto.Hash) ([]byte, error) {
	digestStr, err := checkNotEmptyString(headers, name)
	if err != nil {
		return nil, err
	}
	b, err := base64.RawURLEncoding.DecodeString(digestStr)
	if err != nil {
		return nil, fmt.Errorf("%q header cannot be decoded: %v", name, err)
	}
	if len(b) != h.Size() {
		return nil, fmt.Errorf("%q header does not have the expected bit length: %d", name, len(b)*8)
	}

	return b, nil
}
