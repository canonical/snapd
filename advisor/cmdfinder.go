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

package advisor

import (
	"fmt"
)

type Finder interface {
	Find(command string) (snaps []string, err error)
}

type boltFinder struct{}

func (bf *boltFinder) Find(command string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

var Commands Finder = &boltFinder{}

func ReplaceCommandsFinder(f Finder) (restore func()) {
	old := Commands
	Commands = f
	return func() {
		Commands = old
	}
}
