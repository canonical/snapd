// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package confdb_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	assertstateconfdb "github.com/snapcore/snapd/overlord/assertstate/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type confdbHandlerSuite struct {
	testutil.BaseTest
	st           *state.State
	storeSigning *assertstest.StoreStack
	devSignings  map[string]*assertstest.SigningDB
	view         *confdb.View
	confdbSchema *asserts.ConfdbSchema
}

var _ = Suite(&confdbHandlerSuite{})

func (s *confdbHandlerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.st = state.New(nil)
	_, err := assertstate.Manager(s.st, state.NewTaskRunner(s.st))
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.devSignings = make(map[string]*assertstest.SigningDB)

	var builtinConfdb asserts.Assertion
	for _, as := range asserts.Builtin() {
		if as.Type() == asserts.ConfdbSchemaType && as.HeaderString("name") == "validation-sets" {
			builtinConfdb = as
			break
		}
	}
	c.Assert(builtinConfdb, NotNil)
	s.confdbSchema = builtinConfdb.(*asserts.ConfdbSchema)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: []asserts.Assertion{builtinConfdb},
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(s.storeSigning.StoreAccountKey("")), IsNil)

	assertstate.ReplaceDB(s.st, db)

	// mock a model so ForgetValidationSet doesn't panic
	a := assertstest.FakeAssertion(map[string]any{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: a.(*asserts.Model),
	}
	s.AddCleanup(snapstatetest.MockDeviceContext(deviceCtx))
	s.st.Set("seeded", true)

	s.mockInstalledSnap(c, "enforced-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.R(7))

	s.addValidationSetAssert(c, "my-account", "my-set", 1, []any{
		map[string]any{
			"id":       "cccchntON3vR7kwEbVPsILm7bUViPDcc",
			"name":     "missing-snap",
			"presence": "required",
			"revision": "1",
			"components": map[string]any{
				"my-component": map[string]any{
					"presence": "required",
					"revision": "1",
				},
				"invalid-component": "invalid",
			},
		},
	})

	s.view, err = confdbstate.GetView(s.st, "system", "validation-sets", "admin")
	c.Assert(err, IsNil)
}

// addValidationSetAssert signs and adds a validation-set assertion to the DB.
// Developer accounts and signing keys are created on first use per account.
func (s *confdbHandlerSuite) addValidationSetAssert(c *C, accountID, name string, sequence int, snaps []any) {
	if _, ok := s.devSignings[accountID]; !ok {
		privKey, _ := assertstest.GenerateKey(752)
		acct := assertstest.NewAccount(s.storeSigning, accountID, map[string]any{
			"account-id": accountID,
		}, "")
		c.Assert(assertstate.Add(s.st, acct), IsNil)
		acctKey := assertstest.NewAccountKey(s.storeSigning, acct, nil, privKey.PublicKey(), "")
		c.Assert(assertstate.Add(s.st, acctKey), IsNil)
		s.devSignings[accountID] = assertstest.NewSigningDB(accountID, privKey)
	}

	if snaps == nil {
		snaps = []any{}
	}

	headers := map[string]any{
		"series":       "16",
		"account-id":   accountID,
		"authority-id": accountID,
		"publisher-id": accountID,
		"name":         name,
		"sequence":     fmt.Sprintf("%d", sequence),
		"snaps":        snaps,
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     "1",
	}
	a, err := s.devSignings[accountID].Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.st, a), IsNil)
}

func (s *confdbHandlerSuite) mockInstalledSnap(c *C, name, snapID string, revision snap.Revision) {
	sideInfo := &snap.SideInfo{RealName: name, SnapID: snapID, Revision: revision}
	snaptest.MockSnap(c, fmt.Sprintf("name: %s\nversion: 1", name), sideInfo)
	snapstate.Set(s.st, name, &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  revision,
	})
}

func (s *confdbHandlerSuite) TestDatabagEmpty(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	bag, err := handler.Databag(s.st)
	c.Assert(err, IsNil)
	c.Check(bag, NotNil)

	_, err = s.view.Get(bag, "", nil, confdb.AdminAccess)
	c.Check(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *confdbHandlerSuite) TestDatabagMultipleSetsAndAccounts(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "my-account",
		Name:      "my-set",
		Mode:      assertstate.Monitor,
		Current:   1,
	})
	s.addValidationSetAssert(c, "acct1", "set-b", 4, []any{
		map[string]any{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "enforced-snap",
			"presence": "required",
			"revision": "7",
		},
	})
	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "acct1",
		Name:      "set-b",
		Mode:      assertstate.Enforce,
		PinnedAt:  5,
		Current:   4,
	})
	s.addValidationSetAssert(c, "acct2", "set-c", 1, nil)
	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "acct2",
		Name:      "set-c",
		Mode:      assertstate.Enforce,
		Current:   1,
	})

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	bag, err := handler.Databag(s.st)
	c.Assert(err, IsNil)

	data, err := s.view.Get(bag, "", nil, confdb.AdminAccess)
	c.Assert(err, IsNil)
	c.Check(data, DeepEquals, map[string]any{
		"my-account": map[string]any{
			"my-set": map[string]any{
				"mode":     "monitor",
				"status":   "invalid",
				"sequence": float64(1),
				"revision": float64(1),
				"snaps": []any{
					map[string]any{
						"name":     "missing-snap",
						"id":       "cccchntON3vR7kwEbVPsILm7bUViPDcc",
						"presence": "required",
						"revision": float64(1),
						"components": map[string]any{
							"my-component": map[string]any{
								"presence": "required",
								"revision": float64(1),
							},
							"invalid-component": map[string]any{
								"presence": "invalid",
							},
						},
					},
				},
			},
		},
		"acct1": map[string]any{
			"set-b": map[string]any{
				"mode":            "enforce",
				"status":          "valid",
				"sequence":        float64(4),
				"pinned-sequence": float64(5),
				"revision":        float64(1),
				"snaps": []any{
					map[string]any{
						"name":     "enforced-snap",
						"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
						"presence": "required",
						"revision": float64(7),
					},
				},
			},
		},
		"acct2": map[string]any{
			"set-c": map[string]any{
				"mode":     "enforce",
				"status":   "valid",
				"sequence": float64(1),
				"revision": float64(1),
			},
		},
	})
}

func (s *confdbHandlerSuite) TestUpdateEntireValidationSet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "my-account",
		Name:      "my-set",
		Mode:      assertstate.Monitor,
		Current:   1,
	})

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	// set the entire validation set map
	err = s.view.Set(tx, "my-account.my-set", map[string]any{"mode": "enforce", "pinned-sequence": 5})
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "my-account", "my-set", &tr)
	c.Assert(err, IsNil)
	c.Check(tr.Mode, Equals, assertstate.Enforce)
	c.Check(tr.PinnedAt, Equals, 5)
}

func (s *confdbHandlerSuite) TestCommitUpdatesOnlyMode(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "my-account",
		Name:      "my-set",
		Mode:      assertstate.Enforce,
		PinnedAt:  3,
		Current:   1,
	})

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	// set specific path and check other data isn't affected
	err = s.view.Set(tx, "my-account.my-set.mode", "monitor")
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "my-account", "my-set", &tr)
	c.Assert(err, IsNil)
	c.Check(tr.Mode, Equals, assertstate.Monitor)
	c.Check(tr.PinnedAt, Equals, 3)

	// if we set the entire validation set map without pinned-sequence, it's removed
	tx, err = confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	err = s.view.Set(tx, "my-account.my-set", map[string]any{"mode": "enforce"})
	c.Assert(err, IsNil)

	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	err = assertstate.GetValidationSet(s.st, "my-account", "my-set", &tr)
	c.Assert(err, IsNil)
	c.Check(tr.Mode, Equals, assertstate.Enforce)
	c.Check(tr.PinnedAt, Equals, 0)
}

func (s *confdbHandlerSuite) TestCommitUnsetsPinnedSequence(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "my-account",
		Name:      "my-set",
		Mode:      assertstate.Enforce,
		PinnedAt:  7,
		Current:   1,
	})

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	// unset part of the data
	err = s.view.Unset(tx, "my-account.my-set.pinned-sequence")
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "my-account", "my-set", &tr)
	c.Assert(err, IsNil)
	c.Check(tr.Mode, Equals, assertstate.Enforce)
	c.Check(tr.PinnedAt, Equals, 0)
}

func (s *confdbHandlerSuite) TestCannotUnsetMode(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	err = s.view.Set(tx, "my-account.my-set", map[string]any{
		"mode":            "enforce",
		"pinned-sequence": 5,
	})
	c.Assert(err, IsNil)

	// unsetting mode fails because the storage schema marks it as required
	err = s.view.Unset(tx, "my-account.my-set.mode")
	c.Assert(err, ErrorMatches, `.*cannot find required combinations of keys`)
}

func (s *confdbHandlerSuite) TestCommitForgetsDeletedValidationSet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "my-account",
		Name:      "my-set",
		Mode:      assertstate.Monitor,
		Current:   1,
	})

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	// unset through confdb
	err = s.view.Unset(tx, "my-account.my-set")
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	// check it was deleted from state
	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "my-account", "my-set", &tr)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *confdbHandlerSuite) TestCommitRejectsUnsupportedStorageVersion(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	path, err := confdb.ParsePathIntoAccessors("v2.my-account.my-set.mode", confdb.ParseOptions{})
	c.Assert(err, IsNil)
	err = tx.Set(path, "enforce")
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, ErrorMatches, `internal error: cannot write to system/validation-sets: unsupported storage version "v2"`)
}

func (s *confdbHandlerSuite) TestCommitMultipleSetsAcrossAccounts(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.addValidationSetAssert(c, "acct1", "set-a", 1, nil)
	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "acct1",
		Name:      "set-a",
		Mode:      assertstate.Monitor,
		Current:   1,
	})
	s.addValidationSetAssert(c, "acct1", "set-b", 1, nil)
	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "acct1",
		Name:      "set-b",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   1,
	})
	s.addValidationSetAssert(c, "acct2", "set-c", 1, nil)
	assertstate.UpdateValidationSet(s.st, &assertstate.ValidationSetTracking{
		AccountID: "acct2",
		Name:      "set-c",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   1,
	})

	tx, err := confdbstate.NewTransaction(s.st, "system", "validation-sets")
	c.Assert(err, IsNil)

	err = s.view.Set(tx, "acct1.set-a", map[string]any{
		"mode":            "enforce",
		"pinned-sequence": 10,
	})
	c.Assert(err, IsNil)
	err = s.view.Set(tx, "acct1.set-b", map[string]any{
		"mode":            "monitor",
		"pinned-sequence": 1,
	})
	c.Assert(err, IsNil)
	err = s.view.Set(tx, "acct2.set-c", map[string]any{
		"mode":            "monitor",
		"pinned-sequence": 2,
	})
	c.Assert(err, IsNil)

	handler := &assertstateconfdb.ValsetsConfdbHandler{}
	_, err = handler.Commit(s.st, tx)
	c.Assert(err, IsNil)

	for _, tc := range []struct {
		account string
		name    string
		mode    assertstate.ValidationSetMode
		pin     int
	}{
		{account: "acct1", name: "set-a", mode: assertstate.Enforce, pin: 10},
		{account: "acct1", name: "set-b", mode: assertstate.Monitor, pin: 1},
		{account: "acct2", name: "set-c", mode: assertstate.Monitor, pin: 2},
	} {
		var tr assertstate.ValidationSetTracking
		err = assertstate.GetValidationSet(s.st, tc.account, tc.name, &tr)
		c.Assert(err, IsNil)
		c.Check(tr.Mode, Equals, tc.mode)
		c.Check(tr.Current, Equals, 1)
		c.Check(tr.PinnedAt, Equals, tc.pin)
	}
}
