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
	c.Assert(logger, NotNil)

	type record struct {
		baseAttrs
		Event string `json:"event"`
	}

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
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@gmail.com",
		StoreUserName:  "jdoe",
	}
	reason := seclog.Reason{Code: seclog.ReasonInvalidCredentials, Message: "invalid credentials"}
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
		"User 42:user@gmail.com:jdoe caused an issue: invalid-credentials:invalid credentials")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Type, Equals, "security")
	c.Check(obtained.Category, Equals, "TEST")
	c.Check(obtained.Event, Equals, "test_event")
	// SnapdUser is a LogValuer — verify structured output
	c.Check(obtained.User.ID, Equals, int64(42))
	c.Check(obtained.User.StoreUserEmail, Equals, "user@gmail.com")
	c.Check(obtained.User.StoreUserName, Equals, "jdoe")
	c.Check(obtained.User.Expiration, Equals, "never")
	// Reason is a plain struct — verify JSON marshaling via slog.Any
	c.Check(obtained.Error.Code, Equals, seclog.ReasonInvalidCredentials)
	c.Check(obtained.Error.Message, Equals, "invalid credentials")

	// verify key order for human readability
	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event", "user", "error",
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
