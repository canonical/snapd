// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package backend_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/testutil"
)

type keysSuite struct {
	testutil.BaseTest

	rootdir string
}

var _ = Suite(&keysSuite{})

func (k *keysSuite) TestInMemoryRecoveryKeyStore(c *C) {
	mockRecoveryKey := backend.RecoveryKeyInfo{
		Key:        [16]byte{1, 2, 3, 4},
		Expiration: time.Now(),
	}

	store := backend.NewInMemoryRecoveryKeyStore()

	err := store.AddRecoveryKey("1", mockRecoveryKey)
	c.Assert(err, IsNil)

	rkey, err := store.GetRecoveryKey("1")
	c.Assert(err, IsNil)
	c.Check(rkey, DeepEquals, mockRecoveryKey)

	// cannot add an already existing key
	err = store.AddRecoveryKey("1", backend.RecoveryKeyInfo{})
	c.Assert(err, ErrorMatches, `recovery key with id "1" already exists`)

	err = store.DeleteRecoveryKey("1")
	c.Assert(err, IsNil)

	rkey, err = store.GetRecoveryKey("1")
	c.Assert(err, ErrorMatches, `recovery key with id "1" does not exist`)

	// adding a deleted key works
	err = store.AddRecoveryKey("1", backend.RecoveryKeyInfo{})
	c.Assert(err, IsNil)
}

func (k *keysSuite) TestRecoveryKeyExpired(c *C) {
	now := time.Now()
	rkey := backend.RecoveryKeyInfo{
		Key:        [16]byte{1, 2, 3, 4},
		Expiration: now,
	}

	c.Check(rkey.Expired(now.Add(time.Nanosecond)), Equals, true)
	c.Check(rkey.Expired(now.Add(-time.Nanosecond)), Equals, false)

	// when unset, the key never expires.
	rkey.Expiration = time.Time{}
	c.Check(rkey.Expired(now.Add(10000*time.Hour)), Equals, false)
	c.Check(rkey.Expired(now.Add(10000*time.Hour)), Equals, false)
}
