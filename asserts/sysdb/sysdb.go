// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

// Package sysdb supports the system-wide assertion database with ways to open it and to manage the trusted set of assertions founding it.
package sysdb

import (
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
)

func openDatabaseAt(path string, cfg *asserts.DatabaseConfig) (*asserts.Database, error) {
	bs := mylog.Check2(asserts.OpenFSBackstore(path))

	keypairMgr := mylog.Check2(asserts.OpenFSKeypairManager(path))

	cfg.Backstore = bs
	cfg.KeypairManager = keypairMgr
	return asserts.OpenDatabase(cfg)
}

// OpenAt opens a system assertion database at the given location with
// the trusted assertions set configured.
func OpenAt(path string) (*asserts.Database, error) {
	cfg := &asserts.DatabaseConfig{
		Trusted:         Trusted(),
		OtherPredefined: Generic(),
	}
	return openDatabaseAt(path, cfg)
}

// Open opens the system-wide assertion database with the trusted assertions
// set configured.
func Open() (*asserts.Database, error) {
	return OpenAt(dirs.SnapAssertsDBDir)
}
