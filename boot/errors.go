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

import (
	"errors"
	"fmt"
)

// trySnapError is an error that only applies to the try snaps where multiple
// snaps are returned, this is mainly and primarily used in revisions().
type trySnapError string

func (sre trySnapError) Error() string {
	return string(sre)
}

func newTrySnapErrorf(format string, args ...interface{}) error {
	return trySnapError(fmt.Sprintf(format, args...))
}

// isTrySnapError returns true if the given error is an error resulting from
// accessing information about the try snap or the trying status.
func isTrySnapError(err error) bool {
	switch err.(type) {
	case trySnapError:
		return true
	}
	return false
}

var errTrySnapFallback = errors.New("fallback to original snap")
