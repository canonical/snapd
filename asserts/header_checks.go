// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"regexp"
	"strconv"
	"strings"
	"time"
)

// common checks used when decoding/assembling assertions

func checkExistsString(headers map[string]interface{}, name string) (string, error) {
	return checkExistsStringWhat(headers, name, "header")
}

func checkExistsStringWhat(m map[string]interface{}, name, what string) (string, error) {
	value, ok := m[name]
	if !ok {
		return "", fmt.Errorf("%q %s is mandatory", name, what)
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%q %s must be a string", name, what)
	}
	return s, nil
}

func checkNotEmptyString(headers map[string]interface{}, name string) (string, error) {
	return checkNotEmptyStringWhat(headers, name, "header")
}

func checkNotEmptyStringWhat(m map[string]interface{}, name, what string) (string, error) {
	s, err := checkExistsStringWhat(m, name, what)
	if err != nil {
		return "", err
	}
	if len(s) == 0 {
		return "", fmt.Errorf("%q %s should not be empty", name, what)
	}
	return s, nil
}

func checkOptionalStringWhat(headers map[string]interface{}, name, what string) (string, error) {
	value, ok := headers[name]
	if !ok {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%q %s must be a string", name, what)
	}
	return s, nil
}

func checkOptionalString(headers map[string]interface{}, name string) (string, error) {
	return checkOptionalStringWhat(headers, name, "header")
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
	// validity check against known canonical
	validity := typeRegistry[assertType.Name]
	switch validity {
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
	m, err := atoi(s, "%q %s", name, "header")
	if err != nil {
		return -1, err
	}
	return m, nil
}

func checkInt(headers map[string]interface{}, name string) (int, error) {
	return checkIntWhat(headers, name, "header")
}

func checkIntWhat(headers map[string]interface{}, name, what string) (int, error) {
	valueStr, err := checkNotEmptyStringWhat(headers, name, what)
	if err != nil {
		return -1, err
	}
	value, err := atoi(valueStr, "%q %s", name, what)
	if err != nil {
		return -1, err
	}
	return value, nil
}

type intSyntaxError string

func (e intSyntaxError) Error() string {
	return string(e)
}

func atoi(valueStr, whichFmt string, whichArgs ...interface{}) (int, error) {
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		which := fmt.Sprintf(whichFmt, whichArgs...)
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return -1, fmt.Errorf("%s is out of range: %v", which, valueStr)
		}
		return -1, intSyntaxError(fmt.Sprintf("%s is not an integer: %v", which, valueStr))
	}
	if prefixZeros(valueStr) {
		return -1, fmt.Errorf("%s has invalid prefix zeros: %s", fmt.Sprintf(whichFmt, whichArgs...), valueStr)
	}
	return value, nil
}

func prefixZeros(s string) bool {
	return strings.HasPrefix(s, "0") && s != "0"
}

func checkRFC3339Date(headers map[string]interface{}, name string) (time.Time, error) {
	return checkRFC3339DateWhat(headers, name, "header")
}

func checkRFC3339DateWhat(m map[string]interface{}, name, what string) (time.Time, error) {
	dateStr, err := checkNotEmptyStringWhat(m, name, what)
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q %s is not a RFC3339 date: %v", name, what, err)
	}
	return date, nil
}

func checkRFC3339DateWithDefaultWhat(m map[string]interface{}, name, what string, defl time.Time) (time.Time, error) {
	value, ok := m[name]
	if !ok {
		return defl, nil
	}
	dateStr, ok := value.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("%q %s must be a string", name, what)
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q %s is not a RFC3339 date: %v", name, what, err)
	}
	return date, nil
}

func checkUint(headers map[string]interface{}, name string, bitSize int) (uint64, error) {
	return checkUintWhat(headers, name, bitSize, "header")
}

func checkUintWhat(headers map[string]interface{}, name string, bitSize int, what string) (uint64, error) {
	valueStr, err := checkNotEmptyStringWhat(headers, name, what)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(valueStr, 10, bitSize)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return 0, fmt.Errorf("%q %s is out of range: %v", name, what, valueStr)
		}
		return 0, fmt.Errorf("%q %s is not an unsigned integer: %v", name, what, valueStr)
	}
	if prefixZeros(valueStr) {
		return 0, fmt.Errorf("%q %s has invalid prefix zeros: %s", name, what, valueStr)
	}
	return value, nil
}

func checkDigest(headers map[string]interface{}, name string, h crypto.Hash) ([]byte, error) {
	return checkDigestWhat(headers, name, h, "header")
}

func checkDigestWhat(headers map[string]interface{}, name string, h crypto.Hash, what string) ([]byte, error) {
	digestStr, err := checkNotEmptyStringWhat(headers, name, what)
	if err != nil {
		return nil, err
	}
	b, err := base64.RawURLEncoding.DecodeString(digestStr)
	if err != nil {
		return nil, fmt.Errorf("%q %s cannot be decoded: %v", name, what, err)
	}
	if len(b) != h.Size() {
		return nil, fmt.Errorf("%q %s does not have the expected bit length: %d", name, what, len(b)*8)
	}

	return b, nil
}

// checkStringListInMap returns the `name` entry in the `m` map as a (possibly nil) `[]string`
// if `m` has an entry for `name` and it isn't a `[]string`, an error is returned
// if pattern is not nil, all the strings must match that pattern, otherwise an error is returned
// `what` is a descriptor, used for error messages
func checkStringListInMap(m map[string]interface{}, name, what string, pattern *regexp.Regexp) ([]string, error) {
	value, ok := m[name]
	if !ok {
		return nil, nil
	}
	lst, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be a list of strings", what)
	}
	if len(lst) == 0 {
		return nil, nil
	}
	res := make([]string, len(lst))
	for i, v := range lst {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a list of strings", what)
		}
		if pattern != nil && !pattern.MatchString(s) {
			return nil, fmt.Errorf("%s contains an invalid element: %q", what, s)
		}
		res[i] = s
	}
	return res, nil
}

func checkStringList(headers map[string]interface{}, name string) ([]string, error) {
	return checkStringListMatches(headers, name, nil)
}

func checkStringListMatches(headers map[string]interface{}, name string, pattern *regexp.Regexp) ([]string, error) {
	return checkStringListInMap(headers, name, fmt.Sprintf("%q header", name), pattern)
}

func checkStringMatches(headers map[string]interface{}, name string, pattern *regexp.Regexp) (string, error) {
	return checkStringMatchesWhat(headers, name, "header", pattern)
}

func checkStringMatchesWhat(headers map[string]interface{}, name, what string, pattern *regexp.Regexp) (string, error) {
	s, err := checkNotEmptyStringWhat(headers, name, what)
	if err != nil {
		return "", err
	}
	if !pattern.MatchString(s) {
		return "", fmt.Errorf("%q %s contains invalid characters: %q", name, what, s)
	}
	return s, nil
}

func checkOptionalBool(headers map[string]interface{}, name string) (bool, error) {
	return checkOptionalBoolWhat(headers, name, "header")
}

func checkOptionalBoolWhat(headers map[string]interface{}, name, what string) (bool, error) {
	value, ok := headers[name]
	if !ok {
		return false, nil
	}
	s, ok := value.(string)
	if !ok || (s != "true" && s != "false") {
		return false, fmt.Errorf("%q %s must be 'true' or 'false'", name, what)
	}
	return s == "true", nil
}

func checkMap(headers map[string]interface{}, name string) (map[string]interface{}, error) {
	return checkMapWhat(headers, name, "header")
}

func checkMapWhat(m map[string]interface{}, name, what string) (map[string]interface{}, error) {
	value, ok := m[name]
	if !ok {
		return nil, nil
	}
	mv, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%q %s must be a map", name, what)
	}
	return mv, nil
}
