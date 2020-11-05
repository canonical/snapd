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

package daemon

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
)

func mockSystemRecoveryKey(c *C) {
	// same inputs/outputs as secboot:crypt_test.go in this test
	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	rkeyPath := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	err = os.MkdirAll(filepath.Dir(rkeyPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(rkeyPath, []byte(rkeystr), 0644)
	c.Assert(err, IsNil)
}

func (s *apiSuite) TestSystemGetRecoveryKeyAsRootHappy(c *C) {
	s.daemon(c)
	mockSystemRecoveryKey(c)

	req, err := http.NewRequest("GET", "/v2/system-recovery-key", nil)
	c.Assert(err, IsNil)

	rsp := getSystemRecoveryKeys(systemRecoveryKeysCmd, req, nil).(*resp)
	c.Assert(rsp.Status, Equals, 200)
	srk := rsp.Result.(*client.SystemRecoveryKeysResponse)
	c.Assert(srk, DeepEquals, &client.SystemRecoveryKeysResponse{RecoveryKey: "61665-00531-54469-09783-47273-19035-40077-28287"})
}

func (s *apiSuite) TestSystemGetRecoveryAsUserErrors(c *C) {
	s.daemon(c)
	mockSystemRecoveryKey(c)

	req, err := http.NewRequest("GET", "/v2/system-recovery-key", nil)
	c.Assert(err, IsNil)

	req.RemoteAddr = "pid=100;uid=1000;socket=;"
	rec := httptest.NewRecorder()
	systemsActionCmd.ServeHTTP(rec, req)

	systemRecoveryKeysCmd.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 401)
}
