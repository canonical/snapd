// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = Suite(&systemSecurebootSuite{})

type systemSecurebootSuite struct {
	apiBaseSuite
}

func (s *systemSecurebootSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectRootAccess()
	s.expectWriteAccess(daemon.InterfaceProviderRootAccess{
		Interfaces: []string{"fwupd"},
	})

	s.AddCleanup(daemon.MockFdestateEFISecurebootDBUpdatePrepare(func(st *state.State, db fdestate.EFISecurebootKeyDatabase, payload []byte) error {
		panic("unexpected call")
	}))
	s.AddCleanup(daemon.MockFdestateEFISecurebootDBUpdateCleanup(func(st *state.State) error {
		panic("unexpected call")
	}))
	s.AddCleanup(daemon.MockFdestateEFISecurebootDBManagerStartup(func(st *state.State) error {
		panic("unexpected call")
	}))
}

func (s *systemSecurebootSuite) TestEFISecurebootContentType(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{"action": "blah"}`)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)

	rsp := s.errorReq(c, req, nil, actionIsUnexpected)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `unexpected content type: ""`)
}

func (s *systemSecurebootSuite) TestEFISecurebootBogusAction(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{"action": "blah"}`)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil, actionIsUnexpected)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `unsupported EFI secure boot action "blah"`)
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateStartup(c *C) {
	s.daemon(c)

	startupCalls := 0
	s.AddCleanup(daemon.MockFdestateEFISecurebootDBManagerStartup(func(st *state.State) error {
		startupCalls++
		return nil
	}))

	body := strings.NewReader(`{"action": "efi-secureboot-update-startup"}`)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 200)

	c.Check(startupCalls, Equals, 1)
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBCleanup(c *C) {
	s.daemon(c)

	cleanupCalls := 0
	s.AddCleanup(daemon.MockFdestateEFISecurebootDBUpdateCleanup(func(st *state.State) error {
		cleanupCalls++
		return nil
	}))

	body := strings.NewReader(`{"action": "efi-secureboot-update-db-cleanup"}`)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 200)

	c.Check(cleanupCalls, Equals, 1)
}

func (s *systemSecurebootSuite) testEFISecurebootUpdateDBPrepareNoDataForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	s.daemon(c)

	updateKindStr := kind.String()
	body := strings.NewReader(fmt.Sprintf(`{
 "action": "efi-secureboot-update-db-prepare",
 "key-database": "%s"
}`, updateKindStr))
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 400)
	c.Check(rsp.Message, Matches, "update payload not provided")
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareNoDataPK(c *C) {
	s.testEFISecurebootUpdateDBPrepareNoDataForKind(c, fdestate.EFISecurebootPK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareNoDataKEK(c *C) {
	s.testEFISecurebootUpdateDBPrepareNoDataForKind(c, fdestate.EFISecurebootKEK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareNoDataDB(c *C) {
	s.testEFISecurebootUpdateDBPrepareNoDataForKind(c, fdestate.EFISecurebootDB)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareNoDataDBX(c *C) {
	s.testEFISecurebootUpdateDBPrepareNoDataForKind(c, fdestate.EFISecurebootDBX)
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareBogusDB(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{
 "action": "efi-secureboot-update-db-prepare",
 "key-database": "FOO"
}`)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 400)
	c.Check(rsp.Message, Equals, `invalid key database "FOO"`)
}

func (s *systemSecurebootSuite) testEFISecurebootUpdateDBPrepareBadPayloadForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	s.daemon(c)

	updateKindStr := kind.String()
	body := strings.NewReader(fmt.Sprintf(`{
 "action": "efi-secureboot-update-db-prepare",
 "key-database": "%s",
 "payload": "123"
}`, updateKindStr))
	req, err := http.NewRequest("POST", "/v2/system-secureboot", body)
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 400)
	c.Check(rsp.Message, Matches, `cannot decode payload: illegal base64 .*`)
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareBadPayloadPK(c *C) {
	s.testEFISecurebootUpdateDBPrepareBadPayloadForKind(c, fdestate.EFISecurebootPK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareBadPayloadKEK(c *C) {
	s.testEFISecurebootUpdateDBPrepareBadPayloadForKind(c, fdestate.EFISecurebootKEK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareBadPayloadDB(c *C) {
	s.testEFISecurebootUpdateDBPrepareBadPayloadForKind(c, fdestate.EFISecurebootDB)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareBadPayloadDBX(c *C) {
	s.testEFISecurebootUpdateDBPrepareBadPayloadForKind(c, fdestate.EFISecurebootDBX)
}

func (s *systemSecurebootSuite) testEFISecurebootUpdateDBPrepareHappyForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	s.daemon(c)

	updatePrepareCalls := 0
	s.AddCleanup(daemon.MockFdestateEFISecurebootDBUpdatePrepare(func(st *state.State, db fdestate.EFISecurebootKeyDatabase, payload []byte) error {
		c.Check(db, Equals, kind)
		c.Check(payload, DeepEquals, []byte("payload"))
		updatePrepareCalls++
		return nil
	}))

	updateKindStr := kind.String()
	body, err := json.Marshal(map[string]any{
		"action":       "efi-secureboot-update-db-prepare",
		"key-database": updateKindStr,
		"payload":      base64.StdEncoding.EncodeToString([]byte("payload")),
	})
	c.Assert(err, IsNil)
	req, err := http.NewRequest("POST", "/v2/system-secureboot", bytes.NewReader(body))
	c.Assert(err, IsNil)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Status, Equals, 200)

	c.Check(updatePrepareCalls, Equals, 1)
}

func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareHappyPK(c *C) {
	s.testEFISecurebootUpdateDBPrepareHappyForKind(c, fdestate.EFISecurebootPK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareHappyKEK(c *C) {
	s.testEFISecurebootUpdateDBPrepareHappyForKind(c, fdestate.EFISecurebootKEK)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareHappyDB(c *C) {
	s.testEFISecurebootUpdateDBPrepareHappyForKind(c, fdestate.EFISecurebootDB)
}
func (s *systemSecurebootSuite) TestEFISecurebootUpdateDBPrepareHappyDBX(c *C) {
	s.testEFISecurebootUpdateDBPrepareHappyForKind(c, fdestate.EFISecurebootDBX)
}

func (s *systemSecurebootSuite) TestSecurebootRequestValidate(c *C) {
	r := daemon.SecurebootRequest{
		Action: "foo",
	}
	c.Check(r.Validate(), ErrorMatches, `unsupported EFI secure boot action "foo"`)

	r = daemon.SecurebootRequest{
		Action:      "efi-secureboot-update-startup",
		KeyDatabase: "DBX",
	}
	c.Check(r.Validate(), ErrorMatches, `unexpected key database for action "efi-secureboot-update-startup"`)

	r = daemon.SecurebootRequest{
		Action:  "efi-secureboot-update-db-cleanup",
		Payload: "123",
	}
	c.Check(r.Validate(), ErrorMatches, `unexpected payload for action "efi-secureboot-update-db-cleanup"`)

	r = daemon.SecurebootRequest{
		Action:      "efi-secureboot-update-db-prepare",
		KeyDatabase: "FOO",
	}
	c.Check(r.Validate(), ErrorMatches, `invalid key database "FOO"`)

	r = daemon.SecurebootRequest{
		Action:      "efi-secureboot-update-db-prepare",
		KeyDatabase: "DBX",
	}
	c.Check(r.Validate(), ErrorMatches, `update payload not provided`)

	// valid
	for _, r := range []daemon.SecurebootRequest{{
		Action:      "efi-secureboot-update-db-prepare",
		KeyDatabase: "DBX",
		Payload:     "123",
	}, {
		Action: "efi-secureboot-update-db-cleanup",
	}, {
		Action: "efi-secureboot-update-startup",
	}} {
		c.Logf("testing valid request %+v", r)
		c.Check(r.Validate(), IsNil)
	}
}
