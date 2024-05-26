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
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

type Package struct {
	Snap    string `json:"snap"`
	Version string `json:"version"`
	Summary string `json:"summary,omitempty"`
}

func FindPackage(pkgName string) (*Package, error) {
	finder := mylog.Check2(newFinder())
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}

	defer finder.Close()

	return finder.FindPackage(pkgName)
}
