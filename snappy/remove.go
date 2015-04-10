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

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
)

// RemoveFlags can be used to pass additional flags to the snap removal request
type RemoveFlags uint

const (
	// DoRemoveGC will ensure that garbage collection is done, unless a
	// version is specified.
	DoRemoveGC RemoveFlags = 1 << iota
)

// Remove a part by a partSpec string, this can be "name" or "name=version"
func Remove(partSpec string, flags RemoveFlags) error {
	var parts BySnapVersion

	installed, err := NewMetaRepository().Installed()
	if err != nil {
		return err
	}
	// Note that "=" is not legal in a snap name or a snap version
	l := strings.Split(partSpec, "=")
	if len(l) == 2 {
		name := l[0]
		version := l[1]
		if part := FindSnapByNameAndVersion(name, version, installed); part != nil {
			parts = append(parts, part)
		}
	} else {
		if (flags & DoRemoveGC) == 0 {
			if part := ActiveSnapByName(partSpec); part != nil {
				parts = append(parts, part)
			}
		} else {
			parts = FindSnapsByName(partSpec, installed)
		}
	}

	if len(parts) == 0 {
		return ErrPackageNotFound
	}

	for _, part := range parts {
		pbar := progress.NewTextProgress(part.Name())
		if err := part.Uninstall(pbar); err != nil {
			return logger.LogError(err)
		}
	}

	return nil
}
