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

// TypeAttr describes the type of a capability attribute.
// All capability attributes are assigned with a string value. Internally the
// attribute type can perform type conversion, value validation and value
// transformation, as required by the intended semantics.
type TypeAttr interface {
	// CheckValue checks if the value is correct.
	CheckValue(value interface{}) (interface{}, error)
}
