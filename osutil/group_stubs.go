// -*- Mode: Go; indent-tabs-mode: t -*-

// +build !cgo

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

package osutil

import (
	"errors"
)

func lookupGroup(groupname string) (*Group, error) {
	return nil, errors.New("lookupGroup not implemented for non cgo builds")
}

func lookupGroupByGid(gid uint64) (*Group, error) {
	return nil, errors.New("lookupGroupByGid not implemented for non cgo builds")
}
