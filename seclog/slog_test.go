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

// record is used for basic log event tests.
type record struct {
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

func (s *SlogSuite) TestLogEvent(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)

	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"Something happened",
	)

	var obtained record
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "INFO")
	c.Check(obtained.Description, Equals, "Something happened")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Type, Equals, "security")
	c.Check(obtained.Category, Equals, "TEST")
	c.Check(obtained.Event, Equals, "test_event")

	// verify key order for human readability
	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event",
	})
}

func (s *SlogSuite) TestLogEventWithAttrs(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	type record struct {
		baseAttrs
		Event string `json:"event"`
		User  struct {
			ID             int64  `json:"snapd-user-id"`
			StoreUserName  string `json:"store-user-name"`
			StoreUserEmail string `json:"store-user-email"`
			Expiration     string `json:"expiration"`
		} `json:"user"`
		Error struct {
			Code    int    `json:"code"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@gmail.com",
		StoreUserName:  "jdoe",
	}
	reason := seclog.Reason{Code: 401, Kind: "invalid-credentials", Message: "invalid credentials"}
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelWarn},
		fmt.Sprintf("User %s caused an issue: %s", user.String(), reason.String()),
		seclog.Attr{Key: "user", Value: user},
		seclog.Attr{Key: "error", Value: reason},
	)

	var obtained record
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "WARN")
	c.Check(obtained.Description, Equals,
		"User 42:user@gmail.com:jdoe caused an issue: 401:invalid credentials")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Type, Equals, "security")
	c.Check(obtained.Category, Equals, "TEST")
	c.Check(obtained.Event, Equals, "test_event")
	// SnapdUser is a LogValuer — verify structured output
	c.Check(obtained.User.ID, Equals, int64(42))
	c.Check(obtained.User.StoreUserEmail, Equals, "user@gmail.com")
	c.Check(obtained.User.StoreUserName, Equals, "jdoe")
	c.Check(obtained.User.Expiration, Equals, "never")
	// Reason is a LogValuer — verify structured output
	c.Check(obtained.Error.Code, Equals, 401)
	c.Check(obtained.Error.Kind, Equals, "invalid-credentials")
	c.Check(obtained.Error.Message, Equals, "invalid credentials")

	// verify key order for human readability
	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event", "user", "error",
	})
}

func (s *SlogSuite) TestLogEventWithAuthzAttrs(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)

	type record struct {
		baseAttrs
		Event string `json:"event"`
		Peer  struct {
			Socket string `json:"socket"`
			UID    int64  `json:"uid"`
			PID    int64  `json:"pid"`
		} `json:"peer"`
		Endpoint struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Action        string `json:"action"`
			AccessChecker string `json:"access-checker"`
			AccessLevel   string `json:"access-level"`
		} `json:"endpoint"`
		AuthzChecks struct {
			AccessOptions string `json:"access-options"`
			PeerCreds     string `json:"peer-credentials"`
			Socket        string `json:"socket"`
			Interface     string `json:"interface-requirements"`
			OpenAccess    string `json:"open-access"`
			UserAuth      string `json:"user-authentication"`
			Root          string `json:"root"`
			Polkit        string `json:"polkit"`
		} `json:"authz_checks"`
	}

	peer := seclog.Peer{Socket: "/run/snapd.socket", UID: 0, PID: 4242}
	endpoint := seclog.Endpoint{
		Method:        "POST",
		Path:          "/v2/snaps",
		Action:        "install",
		AccessChecker: "authenticated",
		AccessLevel:   "authenticated",
	}
	checks := seclog.NewAuthzChecks()
	checks.PeerCreds = seclog.AuthzPass

	logger.LogEvent(
		seclog.Event{Category: "AUTHZ", Name: "authz_admin", Level: seclog.LevelInfo},
		"admin access",
		seclog.Attr{Key: "peer", Value: peer},
		seclog.Attr{Key: "endpoint", Value: endpoint},
		seclog.Attr{Key: "authz_checks", Value: checks},
	)

	var obtained record
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.Event, Equals, "authz_admin")
	c.Check(obtained.Peer.Socket, Equals, "/run/snapd.socket")
	c.Check(obtained.Peer.UID, Equals, int64(0))
	c.Check(obtained.Peer.PID, Equals, int64(4242))
	c.Check(obtained.Endpoint.Method, Equals, "POST")
	c.Check(obtained.Endpoint.Path, Equals, "/v2/snaps")
	c.Check(obtained.Endpoint.Action, Equals, "install")
	c.Check(obtained.Endpoint.AccessChecker, Equals, "authenticated")
	c.Check(obtained.Endpoint.AccessLevel, Equals, "authenticated")
	c.Check(obtained.AuthzChecks.AccessOptions, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.PeerCreds, Equals, string(seclog.AuthzPass))
	c.Check(obtained.AuthzChecks.Socket, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Interface, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.OpenAccess, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.UserAuth, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Root, Equals, string(seclog.AuthzNotApplicable))
	c.Check(obtained.AuthzChecks.Polkit, Equals, string(seclog.AuthzNotApplicable))

	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event", "peer", "endpoint", "authz_checks",
	})
}

func (s *SlogSuite) TestLogEventWithLogValuer(c *C) {
	logger := seclog.NewSlogLogger(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	type record struct {
		User struct {
			Expiration string `json:"expiration"`
		} `json:"user"`
	}

	expiry := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	user := seclog.SnapdUser{
		ID:         42,
		Expiration: expiry,
	}
	logger.LogEvent(
		seclog.Event{Category: "TEST", Name: "test_event", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "user", Value: user},
	)

	var obtained record
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained.User.Expiration, Equals, "2026-06-15T12:00:00Z")
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
