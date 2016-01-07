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
)

// stringAttr is a type for storing arbitrary strings in capability attributes.
type stringAttr struct {
}

func (*stringAttr) CheckValue(value interface{}) (interface{}, error) {
	switch value.(type) {
	case string:
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected value of type %T", value)
	}
}
