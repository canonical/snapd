// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package fdestate_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

// state must be locked
func (s *fdeMgrSuite) settle(c *C) {
	s.st.Unlock()
	defer s.st.Lock()
	err := s.o.Settle(testutil.HostScaledTimeout(10 * time.Second))
	c.Assert(err, IsNil)
}

func (s *fdeMgrSuite) TestKeyslotTargetValidate(c *C) {
	k := fdestate.KeyslotTarget{ContainerRole: "system-data", Name: "some-keyslot"}
	c.Assert(k.Validate(), IsNil)

	k = fdestate.KeyslotTarget{ContainerRole: "system-save", Name: "some-other-keyslot"}
	c.Assert(k.Validate(), IsNil)

	k = fdestate.KeyslotTarget{ContainerRole: "some-container", Name: "some-keyslot"}
	c.Assert(k.Validate(), ErrorMatches, `invalid key slot container role "some-container", expected "system-data" or "system-save"`)

	k = fdestate.KeyslotTarget{Name: "some-keyslot"}
	c.Assert(k.Validate(), ErrorMatches, "key slot container role cannot be empty")

	k = fdestate.KeyslotTarget{ContainerRole: "system-save", Name: ""}
	c.Assert(k.Validate(), ErrorMatches, "key slot name cannot be empty")
}

func (s *fdeMgrSuite) TestReplaceRecoveryKey(c *C) {
	keyslots := []fdestate.KeyslotTarget{
		{ContainerRole: "system-data", Name: "default-recovery"},
		{ContainerRole: "system-save", Name: "default-recovery"},
	}

	// initialize fde manager
	onClassic := true
	manager := s.startedManager(c, onClassic)

	_, recoveryKeyID, err := manager.GenerateRecoveryKey()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	chg, err := fdestate.ReplaceRecoveryKey(s.st, recoveryKeyID, keyslots)
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	c.Check(chg.Kind(), Equals, "replace-recovery-key")
	c.Check(chg.Summary(), Matches, "Replace recovery key")
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 1)
	tskReplaceRecoveryKey := tsks[0]
	c.Check(tskReplaceRecoveryKey.Summary(), Matches, "Replace recovery key")
	c.Check(tskReplaceRecoveryKey.Kind(), Equals, "replace-recovery-key")
	var tskRecoveryKeyID string
	err = tskReplaceRecoveryKey.Get("recovery-key-id", &tskRecoveryKeyID)
	c.Assert(err, IsNil)
	c.Assert(tskRecoveryKeyID, Equals, recoveryKeyID)

	s.settle(c)

	// TODO:FDEM: this should intentionally break after "replace-recovery-key" task is implemented
	c.Check(tskReplaceRecoveryKey.Status(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)
}

func (s *fdeMgrSuite) TestReplaceRecoveryKeyErrors(c *C) {
	mockStore := &mockRecoveryKeyCache{
		getRecoveryKey: func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
			switch keyID {
			case "good-key-id":
				return backend.CachedRecoverKey{Expiration: time.Now().Add(100 * time.Hour)}, nil
			case "expired-key-id":
				return backend.CachedRecoverKey{Expiration: time.Now().Add(-100 * time.Hour)}, nil
			default:
				return backend.CachedRecoverKey{}, backend.ErrNoRecoveryKey
			}
		},
	}
	defer fdestate.MockBackendNewInMemoryRecoveryKeyCache(func() backend.RecoveryKeyCache {
		return mockStore
	})()

	// initialize fde manager
	onClassic := true
	s.startedManager(c, onClassic)

	keyslots := []fdestate.KeyslotTarget{
		{ContainerRole: "system-data", Name: "default-recovery"},
		{ContainerRole: "system-save", Name: "default-recovery"},
	}

	s.st.Lock()
	defer s.st.Unlock()

	// invalid recovery key id
	_, err := fdestate.ReplaceRecoveryKey(s.st, "bad-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key id: no recovery key entry for key-id")

	// expired recovery key id
	_, err = fdestate.ReplaceRecoveryKey(s.st, "expired-key-id", keyslots)
	c.Assert(err, ErrorMatches, "invalid recovery key id: recovery key has expired")

	// no keyslots
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", nil)
	c.Assert(err, ErrorMatches, "internal error: keyslots cannot be empty")

	// invalid keyslot
	badKeyslot := fdestate.KeyslotTarget{ContainerRole: "", Name: "some-name"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotTarget{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot \(container-role: "", name: "some-name"\): key slot container role cannot be empty`)

	// invalid keyslot
	badKeyslot = fdestate.KeyslotTarget{ContainerRole: "system-data", Name: "default-fallback"}
	_, err = fdestate.ReplaceRecoveryKey(s.st, "good-key-id", []fdestate.KeyslotTarget{badKeyslot})
	c.Assert(err, ErrorMatches, `invalid key slot \(container-role: "system-data", name: "default-fallback"\): invalid key slot name "default-fallback", expected "default-recovery"`)
}

func (s *fdeMgrSuite) TestEnsureLoopLogging(c *C) {
	testutil.CheckEnsureLoopLogging("fdemgr.go", c, false)
}
