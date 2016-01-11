// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// evalSymlinks is either filepath.EvalSymlinks or a mocked function for
// applicable for testing.
var evalSymlinks = filepath.EvalSymlinks

// pathAttr is a type for storing filesystem paths in capability attributes.
//
// Path is validated by matching it against several regular expressions.  At
// least one regular expression must match for the assignment to succeed.  If
// the stored path contains symbolic links then they are resolved so that the
// final value has no symbolic links (at the time of assignment).
type pathAttr struct {
	// errorHint is a part of the error message when path is invalid.
	// This is better than showing regular expressions to the user.
	errorHint string
	// allowedPatterns describe the set of valid values of path
	allowedPatterns []*regexp.Regexp
}

func (a *pathAttr) CheckValue(value interface{}) (interface{}, error) {
	switch value.(type) {
	case string:
		const errPrefix = "invalid path"
		value := value.(string)
		realValue, err := evalSymlinks(value)
		if err != nil {
			return nil, fmt.Errorf("%s, cannot traverse symbolic links: %s", errPrefix, err)
		}
		valid := false
		for _, pattern := range a.allowedPatterns {
			if pattern.MatchString(value) {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("%s, %s", errPrefix, a.errorHint)
		}
		return realValue, nil
	default:
		return nil, fmt.Errorf("unexpected value of type %T", value)
	}
}
