// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot

import "fmt"

// SnapRevError is an error dealing with boot snap revisions, specifically used
// with revisions() in the bootState interface for example.
type SnapRevError struct {
	Kind    string
	Message string
}

func (sre *SnapRevError) Error() string {
	return sre.Message
}

const (
	// ErrorKindTrySnap is when an error is only for the try snap and can be
	// ignored if a caller is not working with or doesn't need the try snap.
	ErrorKindTrySnap = "try-snap"
)

// trySnapErrorf is a helper for creating a SnapRevError with kind
// ErrorKindTrySnap.
func trySnapErrorf(s string, args ...interface{}) error {
	return &SnapRevError{
		Kind:    ErrorKindTrySnap,
		Message: fmt.Sprintf(s, args...),
	}
}

// IsTrySnapError returns true if the given error is an error resulting from
// accessing information about the try snap or the trying status.
func IsTrySnapError(err error) bool {
	switch e := err.(type) {
	case *SnapRevError:
		return e.Kind == ErrorKindTrySnap
	}
	return false
}
