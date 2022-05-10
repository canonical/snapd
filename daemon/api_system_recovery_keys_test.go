// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package daemon_test

import (
	"encoding/hex"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot/keys"
)

var _ = Suite(&recoveryKeysSuite{})

type recoveryKeysSuite struct {
	apiBaseSuite
}

func (s *recoveryKeysSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectRootAccess()
}

func mockSystemRecoveryKeys(c *C) {
	// same inputs/outputs as secboot:crypt_test.go in this test
	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	rkeyPath := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	err = os.MkdirAll(filepath.Dir(rkeyPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(rkeyPath, []byte(rkeystr), 0644)
	c.Assert(err, IsNil)

	skeystr := "1234567890123456"
	c.Assert(err, IsNil)
	skeyPath := filepath.Join(dirs.SnapFDEDir, "reinstall.key")
	err = ioutil.WriteFile(skeyPath, []byte(skeystr), 0644)
	c.Assert(err, IsNil)
}

func (s *recoveryKeysSuite) TestSystemGetRecoveryKeysAsRootHappy(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)
	mockSystemRecoveryKeys(c)

	req, err := http.NewRequest("GET", "/v2/system-recovery-keys", nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	srk := rsp.Result.(*client.SystemRecoveryKeysResponse)
	c.Assert(srk, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey:  "61665-00531-54469-09783-47273-19035-40077-28287",
		ReinstallKey: "12849-13363-13877-14391-12345-12849-13363-13877",
	})
}

func (s *recoveryKeysSuite) TestSystemGetRecoveryAsUserErrors(c *C) {
	s.daemon(c)
	mockSystemRecoveryKeys(c)

	req, err := http.NewRequest("GET", "/v2/system-recovery-keys", nil)
	c.Assert(err, IsNil)

	// being properly authorized as user is not enough, needs root
	s.asUserAuth(c, req)
	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, Equals, 403)
}
