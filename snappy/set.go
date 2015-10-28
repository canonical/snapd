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

package snappy

import (
	"fmt"
	"strings"

	"github.com/ubuntu-core/snappy/progress"
)

// map from
var setFuncs = map[string]func(k, v string, pb progress.Meter) error{
	"active": makeSnapActiveByNameAndVersion,
}

// SetProperty sets a property for the given pkgname from the args list
func SetProperty(pkgname string, inter progress.Meter, args ...string) (err error) {
	if len(args) < 1 {
		return fmt.Errorf("Need at least one argument for set")
	}

	for _, propVal := range args {
		s := strings.SplitN(propVal, "=", 2)
		if len(s) != 2 {
			return fmt.Errorf("Can not parse property %s", propVal)
		}
		prop := s[0]
		f, ok := setFuncs[prop]
		if !ok {
			return fmt.Errorf("Unknown property %s", prop)
		}
		err := f(pkgname, s[1], inter)
		if err != nil {
			return err
		}
	}

	return err
}
