// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package asserts

import (
	"github.com/snapcore/snapd/dirs"
)

func openDatabaseAt(path string, cfg *DatabaseConfig) (*Database, error) {
	bs, err := OpenFSBackstore(path)
	if err != nil {
		return nil, err
	}
	keypairMgr, err := OpenFSKeypairManager(path)
	if err != nil {
		return nil, err
	}
	cfg.Backstore = bs
	cfg.KeypairManager = keypairMgr
	return OpenDatabase(cfg)
}

// OpenSysDatabase opens the system-wide assertion database with the trusted assertions.
func OpenSysDatabase() (*Database, error) {
	cfg := &DatabaseConfig{
		Trusted: trustedAssertions,
	}
	return openDatabaseAt(dirs.SnapAssertsDBDir, cfg)
}
