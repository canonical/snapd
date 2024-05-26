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

// Package metautil takes care of basic details of working with snap metadata formats.
package metautil

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
)

// NormalizeValue validates values and returns a normalized version of it
// (map[interface{}]interface{} is turned into map[string]interface{})
func NormalizeValue(v interface{}) (interface{}, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return x, nil
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case []interface{}:
		l := make([]interface{}, len(x))
		for i, el := range x {
			el := mylog.Check2(NormalizeValue(el))

			l[i] = el
		}
		return l, nil
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, item := range x {
			kStr, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string key: %v", k)
			}
			item := mylog.Check2(NormalizeValue(item))

			m[kStr] = item
		}
		return m, nil
	case map[string]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, item := range x {
			item := mylog.Check2(NormalizeValue(item))

			m[k] = item
		}
		return m, nil
	default:
		return nil, fmt.Errorf("invalid scalar: %v", v)
	}
}
