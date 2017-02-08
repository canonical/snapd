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
// Strings that do not have the form "k=v" will be dropped.
//
// The result will be sorted list of environment strings.
//
// Circular references (A=$B,B=$A) will result in empty A,B
// (just like in shell)
func SubstituteEnv(env []string) []string {
	envMap := map[string]string{}
	for _, s := range env {
		l := strings.SplitN(s, "=", 2)
		if len(l) < 2 {
			continue
		}
		envMap[l[0]] = l[1]
	}

	// this always terminates, in each iteration of the loop we
	// eliminate at least one value with a "$var"
	for {
		changed := false
		for k, v := range envMap {
			newV := os.Expand(v, func(k string) string {
				if s, ok := envMap[k]; ok {
					return s
				}
				return os.ExpandEnv(v)
			})
			if v != newV {
				changed = true
			}

			envMap[k] = newV
		}
		if !changed {
			break
		}
	}

	allEnv := make([]string, 0, len(envMap))
	for k, v := range envMap {
		allEnv = append(allEnv, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}
	sort.Strings(allEnv)

	return allEnv
}
