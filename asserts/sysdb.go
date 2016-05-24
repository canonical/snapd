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
	"fmt"
	"io/ioutil"

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

// OpenSysDatabase opens the installation-wide assertion database. Uses the given trusted account key.
func OpenSysDatabase(trustedAccountKey string) (*Database, error) {
	var trustedKeys []*AccountKey

	// XXX: temporary, allow to set up without trusted keys
	// until we have and install a firm trusted key with snappy itself
	if trustedAccountKey != "" {
		encodedTrustedAccKey, err := ioutil.ReadFile(trustedAccountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to read trusted account key: %v", err)
		}
		trustedAccKey, err := Decode(encodedTrustedAccKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode trusted account key: %v", err)
		}

		var trustedKey *AccountKey
		switch accKey := trustedAccKey.(type) {
		case *AccountKey:
			trustedKey = accKey
		default:
			return nil, fmt.Errorf("trusted account key is %T, not an account-key", trustedAccKey)
		}
		trustedKeys = []*AccountKey{trustedKey}
	}

	cfg := &DatabaseConfig{
		TrustedKeys: trustedKeys,
	}
	return openDatabaseAt(dirs.SnapAssertsDBDir, cfg)
}
