// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

type patch7Suite struct {
	restoreTrusted func()
}

var _ = Suite(&patch7Suite{})

var statePatch6JSON = []byte(`
{
	"last-task-id": 999,
	"last-change-id": 99,

	"data": {
		"patch-level": 6,
		"snaps": {
			"a": {
				"sequence": [{"name": "a", "revision": "2"}],
				"current": "2"},
			"b": {
				"sequence": [{"name": "b", "revision": "2"}],
				"current": "2"},
			"c": {
				"sequence": [{"name": "c", "revision": "2"}],
                                "jailmode": true,
				"current": "2"}
		},
                "auth": {
                       "device": {
                                 "brand": "my-brand",
                                 "model": "my-model"
                       }
                }
	}
}`)

func (s *patch7Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch6JSON, 0644)
	c.Assert(err, IsNil)

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)

	brandPrivKey, _ := assertstest.GenerateKey(752)

	storeSigning := assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)

	brandAcct := assertstest.NewAccount(storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	brandAccKey := assertstest.NewAccountKey(storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model, err := brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"store":          "my-brand-store-id",
		"gadget":         "gadget",
		"kernel":         "krnl",
		"required-snaps": []interface{}{"a", "b"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.restoreTrusted = sysdb.InjectTrusted(storeSigning.Trusted)

	db, err := sysdb.Open()
	c.Assert(err, IsNil)

	err = db.Add(storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = db.Add(brandAcct)
	c.Assert(err, IsNil)

	err = db.Add(brandAccKey)
	c.Assert(err, IsNil)

	err = db.Add(model)
	c.Assert(err, IsNil)
}

func (s *patch7Suite) TearDownTest(c *C) {
	s.restoreTrusted()
}

func (s *patch7Suite) TestPatch7(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()
	r2 := patch.MockLevel(7)
	defer r2()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	c.Assert(patch.Apply(st), IsNil)

	st.Lock()
	defer st.Unlock()

	var stateMap map[string]map[string]interface{}
	err = st.Get("snaps", &stateMap)
	c.Assert(err, IsNil)

	c.Assert(stateMap, HasLen, 3)

	c.Check(stateMap["a"], DeepEquals, map[string]interface{}{
		"sequence": []interface{}{map[string]interface{}{"name": "a", "revision": "2"}},
		"current":  "2",
		"required": true,
	})
	c.Check(stateMap["b"], DeepEquals, map[string]interface{}{
		"sequence": []interface{}{map[string]interface{}{"name": "b", "revision": "2"}},
		"current":  "2",
		"required": true,
	})
	c.Check(stateMap["c"], DeepEquals, map[string]interface{}{
		"sequence": []interface{}{map[string]interface{}{"name": "c", "revision": "2"}},
		"current":  "2",
		"jailmode": true,
	})
}
