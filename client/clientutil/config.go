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

package clientutil

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
)

// ParseConfigOptions controls how config values should be parsed.
type ParseConfigOptions struct {
	// String is enabled when values should be stored as-is w/o parsing being parsed.
	String bool
	// Typed is enabled when values should be stored parsed as JSON. If String is
	// enabled, this value is ignored.
	Typed bool
}

// ParseConfigValues parses config values in the format of "foo=bar" or "!foo",
// optionally a strict strings or JSON values depending on passed options.
// By default, values are parsed if valid JSON and stored as-is if not.
// Returns a map of config keys to values to set and a slice of keys in the order
// they were passed in.
func ParseConfigValues(confValues []string, opts *ParseConfigOptions) (map[string]interface{}, []string, error) {
	if opts == nil {
		opts = &ParseConfigOptions{}
	}

	patchValues := make(map[string]interface{}, len(confValues))
	keys := make([]string, 0, len(confValues))
	for _, patchValue := range confValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) == 1 && strings.HasSuffix(patchValue, "!") {
			key := strings.TrimSuffix(patchValue, "!")
			patchValues[key] = nil
			keys = append(keys, key)
			continue
		}

		if len(parts) != 2 {
			return nil, nil, fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
		}

		if opts.String {
			patchValues[parts[0]] = parts[1]
		} else {
			var value interface{}
			if err := jsonutil.DecodeWithNumber(strings.NewReader(parts[1]), &value); err != nil {
				if opts.Typed {
					return nil, nil, fmt.Errorf(i18n.G("failed to parse JSON: %w"), err)
				}

				// Not valid JSON-- just save the string as-is.
				patchValues[parts[0]] = parts[1]
			} else {
				patchValues[parts[0]] = value
			}
		}
		keys = append(keys, parts[0])
	}

	return patchValues, keys, nil
}
