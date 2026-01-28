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
	"encoding/json"
	"errors"
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
func ParseConfigValues(confValues []string, opts *ParseConfigOptions) (map[string]any, []string, error) {
	if opts == nil {
		opts = &ParseConfigOptions{}
	}

	patchValues := make(map[string]any, len(confValues))
	keys := make([]string, 0, len(confValues))
	for _, patchValue := range confValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) == 1 && strings.HasSuffix(patchValue, "!") {
			key := strings.TrimSuffix(patchValue, "!")
			if key == "" {
				return nil, nil, errors.New(i18n.G("configuration keys cannot be empty (use key! to unset a key)"))
			}

			patchValues[key] = nil
			keys = append(keys, key)
			continue
		}

		if len(parts) != 2 {
			return nil, nil, fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
		}

		if parts[0] == "" {
			return nil, nil, errors.New(i18n.G("configuration keys cannot be empty"))
		}

		if opts.String {
			patchValues[parts[0]] = parts[1]
		} else {
			var value any
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

type ConfdbOptions struct {
	Typed bool
}

// ParseConfdbConstraints parses --with constraints used to filter snapctl/snap
// get requests.
func ParseConfdbConstraints(with []string, opts ConfdbOptions) (map[string]any, error) {
	if len(with) == 0 {
		return nil, nil
	}

	constraints := make(map[string]any, len(with))
	for _, constraint := range with {
		parts := strings.SplitN(constraint, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf(`--with constraints must be in the form <param>=<constraint> but got %q instead`, constraint)
		}

		var cstrVal any
		if err := json.Unmarshal([]byte(parts[1]), &cstrVal); err != nil {
			var merr *json.SyntaxError
			if !errors.As(err, &merr) {
				// can only happen due to programmer error
				return nil, fmt.Errorf("internal error: cannot unmarshal --with constraint: %v", err)
			}

			if opts.Typed {
				return nil, fmt.Errorf("cannot unmarshal constraint as JSON as required by -t flag: %s", parts[1])
			}

			// fallback to interpreting the value as a string
			cstrVal = parts[1]
		}

		// check if the constraint is valid JSON but of a type we don't accept
		switch cstrVal.(type) {
		case nil, []any, map[string]any:
			if opts.Typed {
				return nil, fmt.Errorf("--with constraints cannot take non-scalar JSON constraint: %s", parts[1])
			}
			cstrVal = parts[1]
		}

		constraints[parts[0]] = cstrVal
	}

	return constraints, nil
}
