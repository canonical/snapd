// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

// The errortypes package defines common error types which are used across the
// prompting subsystems, along with constructors for specific errors based on
// those broader types. Whenever possible, the error definitions and aliases
// defined in interfaces/prompting/errors.go should be used when returning
// errors. The exception is the interfaces/prompting/patterns package, which
// must use the error constructors defined here directly in order to avoid a
// circular dependency on the interfaces/prompting package.
package errortypes

import (
	"errors"
	"fmt"
)

// ErrInvalidPathPattern indicates that an invalid pattern was received.
//
// This error constructor is defined here instead of in interfaces/prompting
// so that it can be used by interfaces/prompting/patterns without a circular
// import. Whenever possible, use the alias defined in interfaces/propting.
func ErrInvalidPathPattern(invalid string, reason string) *ParseError {
	return &ParseError{
		Field:   "path-pattern",
		Msg:     fmt.Sprintf("invalid path pattern: %s: %q", reason, invalid),
		Invalid: invalid,
	}
}

// Marker for UnsupportedValueError, should never be returned as an actual
// error value.
var ErrUnsupportedValue = errors.New("unsupported value")

// UnsupportedValueError is a wrapper for errors about a field having an
// unsupported value when there is a fixed set of supported values.
type UnsupportedValueError struct {
	Field       string
	Msg         string
	Unsupported interface{} // either string or []string
	Supported   []string
}

func (e *UnsupportedValueError) Error() string {
	return e.Msg
}

func (e *UnsupportedValueError) Is(target error) bool {
	return target == ErrUnsupportedValue
}

// Marker for ParseError, should never be returned as an actual error value.
var ErrParseError = errors.New("parse error")

// ParseError is a wrapper for errors about a field whose value is malformed.
type ParseError struct {
	Field   string
	Msg     string
	Invalid string
}

func (e *ParseError) Error() string {
	return e.Msg
}

func (e *ParseError) Is(target error) bool {
	return target == ErrParseError
}
