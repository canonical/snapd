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

package assertstate_test

import (
	"encoding/json"
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

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
	s.st = state.New(nil)

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

	s.addValidationSetAssert(c, "my-account", "my-set", 1, []any{
		map[string]any{
			"id":       "mysnapididididididididididididid",
			"name":     "my-snap",
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

func (s *confdbHandlerSuite) TestDatabagEmpty(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &assertstate.ValsetsConfdbHandler{}
	bag, err := handler.Databag(s.st)
	c.Assert(err, IsNil)
	c.Check(bag, NotNil)

	raw, err := bag.Data()
	c.Assert(err, IsNil)
	c.Check(string(raw), Equals, `{}`)
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
	s.addValidationSetAssert(c, "acct1", "set-b", 4, nil)
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

	handler := &assertstate.ValsetsConfdbHandler{}
	bag, err := handler.Databag(s.st)
	c.Assert(err, IsNil)

	var data map[string]map[string]map[string]any
	err = json.Unmarshal(bag["v1"], &data)
	c.Assert(err, IsNil)
	c.Check(data, DeepEquals, map[string]map[string]map[string]any{
		"my-account": {
			"my-set": {
				"mode":     "monitor",
				"sequence": float64(1),
				"revision": float64(1),
				"snaps": []any{
					map[string]any{
						"name":     "my-snap",
						"id":       "mysnapididididididididididididid",
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
		// snap and component constraints are omitted from these two for conciseness
		"acct1": {
			"set-b": {
				"mode":            "enforce",
				"sequence":        float64(4),
				"pinned-sequence": float64(5),
				"revision":        float64(1),
			},
		},
		"acct2": {
			"set-c": {
				"mode":     "enforce",
				"sequence": float64(1),
				"revision": float64(1),
			},
		},
	})
}
