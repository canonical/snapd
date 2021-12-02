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

package main

import (
	"fmt"

	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
)

type cmdMakeRefreshable struct {
	TopDir     string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
	SnapBlob   string `long:"snap-blob" description:"File or directory with new snap revision contents"`
	Positional struct {
		SnapName string `description:"snap name" positional-arg-name:"snap-name"`
	} `positional-args:"yes" required:"1"`
}

func (x *cmdMakeRefreshable) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected additional arguments %v", args)
	}
	// setup fake new revisions of snaps for refresh
	return refresh.MakeFakeRefreshForSnaps(x.Positional.SnapName, x.TopDir, x.SnapBlob)
}

var shortMakeRefreshableHelp = "Makes new versions of the given snaps"

func init() {
	parser.AddCommand("make-refreshable", shortMakeRefreshableHelp, "", &cmdMakeRefreshable{})
}
