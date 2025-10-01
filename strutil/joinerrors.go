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

package strutil

import (
	"fmt"
)

// JoinErrors returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if every value in errs is nil.
//
// TODO:GOVERSION: replace with errors.Join() once we're on golang v1.20+
//
// This is a lossy implementation of errors.Join, where only the first non-nil
// error is preserved in a state which can be unwrapped.
func JoinErrors(errs ...error) error {
	var nonNilErrs []error
	for _, e := range errs {
		if e != nil {
			nonNilErrs = append(nonNilErrs, e)
		}
	}
	if len(nonNilErrs) == 0 {
		return nil
	}
	err := nonNilErrs[0]
	for _, e := range nonNilErrs[1:] {
		err = fmt.Errorf("%w\n%v", err, e)
	}
	return err
}
