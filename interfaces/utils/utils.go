// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package utils

import (
	"encoding/json"

	"github.com/ddkwork/golibrary/mylog"
)

// NormalizeInterfaceAttributes normalises types of an attribute values.
// The following transformations are applied: int -> int64, float32 -> float64.
// The normalisation proceeds recursively through maps and slices.
func NormalizeInterfaceAttributes(value interface{}) interface{} {
	// Normalize ints/floats using their 64-bit variants.
	switch v := value.(type) {
	case int:
		return int64(v)
	case float32:
		return float64(v)
	case []interface{}:
		vc := make([]interface{}, len(v))
		for i, el := range v {
			vc[i] = NormalizeInterfaceAttributes(el)
		}
		return vc
	case map[string]interface{}:
		vc := make(map[string]interface{}, len(v))
		for key, item := range v {
			vc[key] = NormalizeInterfaceAttributes(item)
		}
		return vc
	case json.Number:
		jsonval := value.(json.Number)
		if asInt := mylog.Check2(jsonval.Int64()); err == nil {
			return asInt
		}
		asFloat, _ := jsonval.Float64()
		return asFloat
	}
	return value
}

// CopyAttributes makes a deep copy of the attributes map.
func CopyAttributes(value map[string]interface{}) map[string]interface{} {
	return copyRecursive(value).(map[string]interface{})
}

func copyRecursive(value interface{}) interface{} {
	// note: ensure all the mutable types (or types that need a conversion)
	// are handled here.
	switch v := value.(type) {
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, el := range v {
			arr[i] = copyRecursive(el)
		}
		return arr
	case map[string]interface{}:
		mp := make(map[string]interface{}, len(v))
		for key, item := range v {
			mp[key] = copyRecursive(item)
		}
		return mp
	}
	return value
}
