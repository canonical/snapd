// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

func NormalizeInterfaceAttributes(value interface{}) interface{} {
	// Normalize ints/floats using their 64-bit variants.
	// That kind of normalization happens in normalizeYamlValue(..) for static attributes
	// when the yaml is loaded, but it needs to be done here as well because we're also
	// dealing with dynamic attributes set by the code of interfaces.
	switch v := value.(type) {
	case int:
		return int64(v)
	case float32:
		return float64(v)
	case []interface{}:
		for i, el := range v {
			v[i] = NormalizeInterfaceAttributes(el)
		}
	case map[string]interface{}:
		for key, item := range v {
			v[key] = NormalizeInterfaceAttributes(item)
		}
	}
	return value
}
