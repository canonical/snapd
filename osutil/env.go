// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// GetenvBool returns whether the given key may be considered "set" in the
// environment (i.e. it is set to one of "1", "true", etc).
//
// An optional second argument can be provided, which determines how to
// treat missing or unparsable values; default is to treat them as false.
func GetenvBool(key string, dflt ...bool) bool {
	val := os.Getenv(key)
	if val == "" {
		if len(dflt) > 0 {
			return dflt[0]
		}

		return false
	}

	b, err := strconv.ParseBool(val)
	if err != nil {
		if len(dflt) > 0 {
			return dflt[0]
		}

		return false
	}

	return b
}

// SubstituteEnv takes a list of environment strings like:
// - K1=BAR
// - K2=$BAR
// - K3=${BAZ}
// and substitutes them using the os environment strings.
//
// The result will be sorted list of environment strings or
// an error if there are circular refrences.
func SubstituteEnv(env []string) ([]string, error) {
	var allEnv []string

	envMap := map[string]string{}
	for _, s := range env {
		l := strings.SplitN(s, "=", 2)
		if len(l) < 2 {
			return nil, fmt.Errorf("invalid environment string %q", s)
		}
		envMap[l[0]] = l[1]
	}

	for _, s := range env {
		env := os.Expand(s, func(k string) string {
			if s, ok := envMap[k]; ok {
				return s
			}
			return os.Getenv(k)
		})

		allEnv = append(allEnv, env)
	}
	sort.Strings(allEnv)

	return allEnv, nil
}
