// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"time"
)

// common checkers used when decoding/building assertions

func checkMandatory(headers map[string]string, name string) (string, error) {
	value, ok := headers[name]
	if !ok {
		return "", fmt.Errorf("%q header is mandatory", name)
	}
	if len(value) == 0 {
		return "", fmt.Errorf("%q should not be empty", name)
	}
	return value, nil
}

func checkAssertType(assertType AssertionType) (*assertionTypeRegistration, error) {
	reg := typeRegistry[assertType]
	if reg == nil {
		return nil, fmt.Errorf("unknown assertion type: %v", assertType)
	}
	return reg, nil
}

func checkRFC3339Date(headers map[string]string, name string) (time.Time, error) {
	dateStr, err := checkMandatory(headers, name)
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q header is not a RFC3339 date: %v", name, err)
	}
	return date, nil
}
