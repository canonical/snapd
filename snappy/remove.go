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
	"strings"
)

// Remove a part by a partSpec string, this can be "name" or "name=version"
func Remove(partSpec string) error {
	var part Part
	// Note that "=" is not legal in a snap name or a snap version
	l := strings.Split(partSpec, "=")
	if len(l) == 2 {
		name := l[0]
		version := l[1]
		installed, err := NewMetaRepository().Installed()
		if err != nil {
			return err
		}
		part = FindSnapByNameAndVersion(name, version, installed)
	} else {
		part = ActiveSnapByName(partSpec)
	}

	if part == nil {
		return ErrPackageNotFound
	}

	return part.Uninstall()
}
