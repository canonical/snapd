// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"bytes"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
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
	rkeystr := mylog.Check2(hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e"))

	rkeyPath := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	mylog.Check(os.MkdirAll(filepath.Dir(rkeyPath), 0755))

	mylog.Check(os.WriteFile(rkeyPath, []byte(rkeystr), 0644))


	skeystr := "1234567890123456"

	skeyPath := filepath.Join(dirs.SnapFDEDir, "reinstall.key")
	mylog.Check(os.WriteFile(skeyPath, []byte(skeystr), 0644))

}

func (s *recoveryKeysSuite) TestGetSystemRecoveryKeysAsRootHappy(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)
	mockSystemRecoveryKeys(c)

	req := mylog.Check2(http.NewRequest("GET", "/v2/system-recovery-keys", nil))


	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	srk := rsp.Result.(*client.SystemRecoveryKeysResponse)
	c.Assert(srk, DeepEquals, &client.SystemRecoveryKeysResponse{
		RecoveryKey:  "61665-00531-54469-09783-47273-19035-40077-28287",
		ReinstallKey: "12849-13363-13877-14391-12345-12849-13363-13877",
	})
}

func (s *recoveryKeysSuite) TestGetSystemRecoveryKeysAsUserErrors(c *C) {
	s.daemon(c)
	mockSystemRecoveryKeys(c)

	req := mylog.Check2(http.NewRequest("GET", "/v2/system-recovery-keys", nil))


	// being properly authorized as user is not enough, needs root
	s.asUserAuth(c, req)
	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, Equals, 403)
}

func (s *recoveryKeysSuite) TestPostSystemRecoveryKeysActionRemove(c *C) {
	s.daemon(c)

	called := 0
	defer daemon.MockDeviceManagerRemoveRecoveryKeys(func() error {
		called++
		return nil
	})()

	buf := bytes.NewBufferString(`{"action":"remove"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/system-recovery-keys", buf))

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)
	c.Check(called, Equals, 1)
}

func (s *recoveryKeysSuite) TestPostSystemRecoveryKeysAsUserErrors(c *C) {
	s.daemon(c)
	mockSystemRecoveryKeys(c)

	req := mylog.Check2(http.NewRequest("POST", "/v2/system-recovery-keys", nil))


	// being properly authorized as user is not enough, needs root
	s.asUserAuth(c, req)
	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, Equals, 403)
}

func (s *recoveryKeysSuite) TestPostSystemRecoveryKeysBadAction(c *C) {
	s.daemon(c)

	called := 0
	defer daemon.MockDeviceManagerRemoveRecoveryKeys(func() error {
		called++
		return nil
	})()

	buf := bytes.NewBufferString(`{"action":"unknown"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/system-recovery-keys", buf))


	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, DeepEquals, daemon.BadRequest(`unsupported recovery keys action "unknown"`))
	c.Check(called, Equals, 0)
}

func (s *recoveryKeysSuite) TestPostSystemRecoveryKeysActionRemoveError(c *C) {
	s.daemon(c)

	called := 0
	defer daemon.MockDeviceManagerRemoveRecoveryKeys(func() error {
		called++
		return errors.New("boom")
	})()

	buf := bytes.NewBufferString(`{"action":"remove"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/system-recovery-keys", buf))


	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, DeepEquals, daemon.InternalError("boom"))
	c.Check(called, Equals, 1)
}
