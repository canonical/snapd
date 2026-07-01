// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build go1.21 && !noslog

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

// slog.go coverage (all tests in this file):
//   NewSlogLogger, LogEvent envelope  → TestNewSlogLogger, TestLogEventEnvelope
//   Key order                           → TestLogEventKeyOrder
//   Level filtering                     → TestLevelFiltering
//   Write failure handling              → TestWriteFailure*
//   SnapdUser.LogValue                  → TestSnapdUserLogValue
//   Reason.LogValue                     → TestReasonLogValue
//   Peer.LogValue                       → TestPeerLogValue
//   Endpoint.LogValue                   → TestEndpointLogValue
//   AuthzChecks.LogValue                → TestAuthzChecksLogValue
//   AddOptions.LogValue                 → TestAddOptionsLogValue
//   Ref.LogValue                        → TestRefLogValue
//   RemoveOptions.LogValue              → TestRemoveOptionsLogValue

package seclog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

type SlogSuite struct {
	testutil.BaseTest
	buf   *bytes.Buffer
	appID string
}

var _ = Suite(&SlogSuite{})

func (s *SlogSuite) SetUpSuite(c *C) {
	s.buf = &bytes.Buffer{}
	s.appID = "canonical.snapd"
}

func (s *SlogSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.buf.Reset()
}

func (s *SlogSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *SlogSuite) newLogger(c *C) seclog.SecurityLogger {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)
	return logger
}

func (s *SlogSuite) TestNewSlogLogger(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)
	c.Check(logger, NotNil)
}

// baseAttrs represents the non-optional attributes that are present in
// every record.
type baseAttrs struct {
	Datetime    time.Time `json:"datetime"`
	Level       string    `json:"level"`
	Description string    `json:"description"`
	AppID       string    `json:"app_id"`
	Type        string    `json:"type"`
	Category    string    `json:"category"`
}

// envelopeRecord is used for envelope-only log event tests.
type envelopeRecord struct {
	baseAttrs
	Event string `json:"event"`
}

// orderedKeys extracts the top-level JSON object keys in order.
func orderedKeys(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	// consume opening '{'
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("expected '{' delimiter")
	}
	var keys []string
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("expected string key")
		}
		keys = append(keys, key)
		// skip value
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func (s *SlogSuite) TestLogEventEnvelope(c *C) {
	logger := s.newLogger(c)

	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"Something happened",
	)

	var obtained envelopeRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "INFO")
	c.Check(obtained.Description, Equals, "Something happened")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Type, Equals, "security")
	c.Check(obtained.Category, Equals, "TEST")
	c.Check(obtained.Event, Equals, "test_event")
}

func (s *SlogSuite) TestLogEventKeyOrder(c *C) {
	cases := []struct {
		attrs    []seclog.Attr
		wantKeys []string
	}{
		{
			attrs: nil,
			wantKeys: []string{
				"datetime", "level", "description",
				"app_id", "type", "category", "event",
			},
		},
		{
			attrs: []seclog.Attr{
				{Key: "user", Value: seclog.SnapdUser{ID: 1}},
				{Key: "error", Value: seclog.Reason{Code: 401, Message: "nope"}},
			},
			wantKeys: []string{
				"datetime", "level", "description",
				"app_id", "type", "category", "event", "user", "error",
			},
		},
		{
			attrs: []seclog.Attr{
				{Key: "peer", Value: seclog.Peer{Socket: "/run/snapd.socket"}},
				{Key: "endpoint", Value: seclog.Endpoint{Method: "GET", Path: "/v2/snaps"}},
				{Key: "authz_checks", Value: seclog.NewAuthzChecks()},
			},
			wantKeys: []string{
				"datetime", "level", "description",
				"app_id", "type", "category", "event", "peer", "endpoint", "authz_checks",
			},
		},
	}

	for _, tc := range cases {
		s.buf.Reset()
		logger := s.newLogger(c)
		logger.LogEvent(
			seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
			"test",
			tc.attrs...,
		)

		keys, err := orderedKeys(s.buf.Bytes())
		c.Assert(err, IsNil)
		c.Check(keys, DeepEquals, tc.wantKeys)
	}
}

func (s *SlogSuite) TestSnapdUserLogValue(c *C) {
	type userRecord struct {
		User struct {
			ID             int64  `json:"snapd_user_id"`
			StoreUserName  string `json:"store_user_name"`
			StoreUserEmail string `json:"store_user_email"`
			Expiration     string `json:"expiration"`
		} `json:"user"`
	}

	cases := []struct {
		user     seclog.SnapdUser
		wantExp  string
		wantID   int64
		wantName string
		wantMail string
	}{
		{
			user: seclog.SnapdUser{
				ID:             42,
				StoreUserEmail: "user@gmail.com",
				StoreUserName:  "jdoe",
			},
			wantExp:  "never",
			wantID:   42,
			wantName: "jdoe",
			wantMail: "user@gmail.com",
		},
		{
			user: seclog.SnapdUser{
				ID:         42,
				Expiration: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			},
			wantExp: "2026-06-15T12:00:00Z",
			wantID:  42,
		},
	}

	for _, tc := range cases {
		s.buf.Reset()
		logger := s.newLogger(c)
		logger.LogEvent(
			seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
			"test",
			seclog.Attr{Key: "user", Value: tc.user},
		)

		var obtained userRecord
		err := json.Unmarshal(s.buf.Bytes(), &obtained)
		c.Assert(err, IsNil)
		c.Check(obtained.User.ID, Equals, tc.wantID)
		c.Check(obtained.User.StoreUserName, Equals, tc.wantName)
		c.Check(obtained.User.StoreUserEmail, Equals, tc.wantMail)
		c.Check(obtained.User.Expiration, Equals, tc.wantExp)
	}
}

func (s *SlogSuite) TestReasonLogValue(c *C) {
	type errorRecord struct {
		Error struct {
			Code    int    `json:"code"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}

	cases := []struct {
		reason      seclog.Reason
		wantCode    int
		wantKind    string
		wantMessage string
	}{
		{
			reason:      seclog.Reason{Code: 401, Kind: "invalid-credentials", Message: "invalid credentials"},
			wantCode:    401,
			wantKind:    "invalid-credentials",
			wantMessage: "invalid credentials",
		},
		{
			reason:      seclog.Reason{Code: 500, Message: "internal error"},
			wantCode:    500,
			wantMessage: "internal error",
		},
	}

	for _, tc := range cases {
		s.buf.Reset()
		logger := s.newLogger(c)
		logger.LogEvent(
			seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
			"test",
			seclog.Attr{Key: "error", Value: tc.reason},
		)

		var obtained errorRecord
		err := json.Unmarshal(s.buf.Bytes(), &obtained)
		c.Assert(err, IsNil)
		c.Check(obtained.Error.Code, Equals, tc.wantCode)
		c.Check(obtained.Error.Kind, Equals, tc.wantKind)
		c.Check(obtained.Error.Message, Equals, tc.wantMessage)
	}
}

func (s *SlogSuite) TestPeerLogValue(c *C) {
	type peerRecord struct {
		Peer struct {
			Socket string `json:"socket"`
			UID    int64  `json:"uid"`
			PID    int64  `json:"pid"`
		} `json:"peer"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "peer", Value: seclog.Peer{
			Socket: "/run/snapd.socket", UID: 0, PID: 4242,
		}},
	)

	var obtained peerRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.Peer.Socket, Equals, "/run/snapd.socket")
	c.Check(obtained.Peer.UID, Equals, int64(0))
	c.Check(obtained.Peer.PID, Equals, int64(4242))
}

func (s *SlogSuite) TestEndpointLogValue(c *C) {
	type endpointRecord struct {
		Endpoint struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Action        string `json:"action"`
			AccessChecker string `json:"access_checker"`
			AccessLevel   string `json:"access_level"`
		} `json:"endpoint"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "endpoint", Value: seclog.Endpoint{
			Method:        "POST",
			Path:          "/v2/snaps",
			Action:        "install",
			AccessChecker: "authenticated",
			AccessLevel:   "authenticated",
		}},
	)

	var obtained endpointRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.Endpoint.Method, Equals, "POST")
	c.Check(obtained.Endpoint.Path, Equals, "/v2/snaps")
	c.Check(obtained.Endpoint.Action, Equals, "install")
	c.Check(obtained.Endpoint.AccessChecker, Equals, "authenticated")
	c.Check(obtained.Endpoint.AccessLevel, Equals, "authenticated")
}

func (s *SlogSuite) TestAddOptionsLogValue(c *C) {
	type addOptionsRecord struct {
		seclog.AddOptions `json:"add_options"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "add_options", Value: seclog.AddOptions{
			RealUserName:        "Karl Popper",
			Sudoer:              true,
			ExtraUsers:          true,
			ForcePasswordChange: true,
			Known:               false,
		}},
	)

	var obtained addOptionsRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.AddOptions.RealUserName, Equals, "Karl Popper")
	c.Check(obtained.AddOptions.Sudoer, Equals, true)
	c.Check(obtained.AddOptions.ExtraUsers, Equals, true)
	c.Check(obtained.AddOptions.ForcePasswordChange, Equals, true)
	c.Check(obtained.AddOptions.Known, Equals, false)
}

func (s *SlogSuite) TestRefLogValue(c *C) {
	type refRecord struct {
		Ref seclog.Ref `json:"ref"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "ref", Value: seclog.Ref{
			Type:       "system-user",
			PrimaryKey: []string{"my-brand", "foo@bar.com"},
			Revision:   2,
		}},
	)

	var obtained refRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.Ref.Type, Equals, "system-user")
	c.Check(obtained.Ref.PrimaryKey, DeepEquals, []string{"my-brand", "foo@bar.com"})
	c.Check(obtained.Ref.Revision, Equals, 2)
}

func (s *SlogSuite) TestAddOptionsWithAssertionLogValue(c *C) {
	type addOptionsRecord struct {
		seclog.AddOptions `json:"add_options"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "add_options", Value: seclog.AddOptions{
			Known: true,
			Assertion: &seclog.Ref{
				Type:       "system-user",
				PrimaryKey: []string{"my-brand", "foo@bar.com"},
				Revision:   1,
			},
		}},
	)

	var obtained addOptionsRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.AddOptions.Known, Equals, true)
	c.Assert(obtained.AddOptions.Assertion, NotNil)
	c.Check(obtained.AddOptions.Assertion.Type, Equals, "system-user")
	c.Check(obtained.AddOptions.Assertion.PrimaryKey, DeepEquals, []string{"my-brand", "foo@bar.com"})
	c.Check(obtained.AddOptions.Assertion.Revision, Equals, 1)
}

func (s *SlogSuite) TestRemoveOptionsLogValue(c *C) {
	type removeOptionsRecord struct {
		seclog.RemoveOptions `json:"remove_options"`
	}

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "remove_options", Value: seclog.RemoveOptions{Force: true}},
	)

	var obtained removeOptionsRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.RemoveOptions.Force, Equals, true)
}

func (s *SlogSuite) TestAuthzChecksLogValue(c *C) {
	type authzChecksRecord struct {
		AuthzChecks struct {
			AccessOptions string `json:"access_options"`
			PeerCreds     string `json:"peer_credentials"`
			Socket        string `json:"socket"`
			Interface     string `json:"interface_requirements"`
			OpenAccess    string `json:"open_access"`
			UserAuth      string `json:"user_authentication"`
			Root          string `json:"root"`
			Polkit        string `json:"polkit"`
		} `json:"authz_checks"`
	}

	checks := seclog.NewAuthzChecks()
	checks.PeerCreds = seclog.AuthzPass

	logger := s.newLogger(c)
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "authz_checks", Value: checks},
	)

	var obtained authzChecksRecord
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.AuthzChecks.AccessOptions, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.PeerCreds, Equals, string(seclog.AuthzPass))
	c.Check(obtained.AuthzChecks.Socket, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Interface, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.OpenAccess, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.UserAuth, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Root, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Polkit, Equals, string(seclog.AuthzNotApplicable))
}

func (s *SlogSuite) TestLevelFiltering(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelWarn)
	c.Assert(logger, NotNil)

	// LevelInfo is below LevelWarn — should be filtered out
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"Should be filtered",
	)
	c.Check(s.buf.Len(), Equals, 0)

	// LevelWarn meets the threshold — should be emitted
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelWarn},
		"Should pass",
	)
	c.Check(s.buf.Len() > 0, Equals, true)
}

// failWriter is an io.Writer whose Write method can be toggled to fail.
type failWriter struct {
	buf     bytes.Buffer
	failing bool
}

func (w *failWriter) Write(p []byte) (int, error) {
	if w.failing {
		return 0, fmt.Errorf("disk full")
	}
	return w.buf.Write(p)
}

func (s *SlogSuite) TestWriteFailureIsLogged(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	w := &failWriter{failing: true}
	sl := seclog.NewSlogLogger(w, s.appID, seclog.LevelInfo)

	sl.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"should fail",
	)

	c.Check(logBuf.String(), testutil.Contains, "security log write failed: disk full")
}

func (s *SlogSuite) TestWriteFailureSuppressedAfterThreshold(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	w := &failWriter{failing: true}
	sl := seclog.NewSlogLogger(w, s.appID, seclog.LevelInfo)

	evt := seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo}
	for i := 0; i < 5; i++ {
		sl.LogEvent(evt, fmt.Sprintf("attempt %d", i))
	}

	output := logBuf.String()
	// First 2 failures are reported individually.
	c.Check(strings.Count(output, "security log write failed: disk full"), Equals, 2)
	// The suppression message appears exactly once.
	c.Check(strings.Count(output, "further failures will not be reported"), Equals, 1)
}

func (s *SlogSuite) TestWriteRecoveryIsLogged(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	w := &failWriter{failing: true}
	sl := seclog.NewSlogLogger(w, s.appID, seclog.LevelInfo)

	evt := seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo}
	// Exceed the threshold.
	for i := 0; i < 4; i++ {
		sl.LogEvent(evt, "fail")
	}

	// Recover.
	w.failing = false
	sl.LogEvent(evt, "recovered")

	c.Check(logBuf.String(), testutil.Contains, "security log write recovered following 4 failures")
	c.Check(w.buf.Len() > 0, Equals, true)
}

func (s *SlogSuite) TestNoRecoveryMessageBelowThreshold(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	w := &failWriter{failing: true}
	sl := seclog.NewSlogLogger(w, s.appID, seclog.LevelInfo)

	evt := seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo}
	// Fail twice (below threshold of 3).
	sl.LogEvent(evt, "fail 1")
	sl.LogEvent(evt, "fail 2")

	// Recover — no recovery message expected since threshold was not reached.
	w.failing = false
	sl.LogEvent(evt, "ok")

	c.Check(strings.Contains(logBuf.String(), "security log write recovered"), Equals, false)
}
