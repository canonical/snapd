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
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"log/slog"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

type SlogSuite struct {
	testutil.BaseTest
	buf      *bytes.Buffer
	appID    string
	provider seclog.Provider
}

var _ = Suite(&SlogSuite{})

func TestSlog(t *testing.T) { TestingT(t) }

func (s *SlogSuite) SetUpSuite(c *C) {
	s.buf = &bytes.Buffer{}
	s.appID = "canonical.snapd"
	s.provider = seclog.SlogProvider{}
}

func (s *SlogSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.buf.Reset()
}

func (s *SlogSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

// extractSlogLogger is a test helper to extract the internal [slog.Logger] from
// SecurityLogger.
func extractSlogLogger(logger seclog.SecurityLogger) (*slog.Logger, error) {
	if l, ok := logger.(*seclog.SlogLogger); !ok {
		return nil, errors.New("cannot extract slog logger")
	} else {
		// return the internal slog logger
		return l.SlogLogger(), nil
	}
}

func (s *SlogSuite) TestSlogProvider(c *C) {
	logger := s.provider.New(s.buf, s.appID, seclog.LevelInfo)
	c.Check(logger, NotNil)

	impl := s.provider.Impl()
	c.Check(impl, Equals, seclog.ImplSlog)
}

// baseAttrs represents the non-optional attributes that is present in
// every record
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

type attrsAllTypes struct {
	baseAttrs
	String    string        `json:"string"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
	Float64   float64       `json:"float64"`
	Int64     int64         `json:"int64"`
	Int       int           `json:"int"`
	Uint64    uint64        `json:"uint64"`
	Any       any           `json:"any"`
}

func (s *SlogSuite) TestHandlerAttrsAllTypes(c *C) {
	logger := s.provider.New(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	sl, err := extractSlogLogger(logger)
	c.Assert(err, IsNil)
	sl.LogAttrs(
		context.Background(),
		slog.Level(seclog.LevelInfo),
		"test description",
		slog.Attr{Key: "category", Value: slog.StringValue("AUTHN")},
		slog.Attr{Key: "string", Value: slog.StringValue("test string")},
		slog.Attr{Key: "duration", Value: slog.DurationValue(time.Duration(90 * time.Second))},
		slog.Attr{
			Key:   "timestamp",
			Value: slog.TimeValue(time.Date(2025, 10, 8, 8, 0, 0, 0, time.UTC)),
		},
		slog.Attr{Key: "float64", Value: slog.Float64Value(3.141592653589793)},
		slog.Attr{Key: "int64", Value: slog.Int64Value(-4611686018427387904)},
		slog.Attr{Key: "int", Value: slog.IntValue(-4294967296)},
		slog.Attr{Key: "uint64", Value: slog.Uint64Value(4294967295)},
		// AnyValue returns value of KindInt64, the original
		// numeric type is not preserved
		slog.Attr{Key: "any", Value: slog.AnyValue(map[string]any{"k": "v", "n": int(1)})},
	)

	var obtained attrsAllTypes
	err = json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)

	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "INFO")
	c.Check(obtained.Description, Equals, "test description")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Type, Equals, "security")
	c.Check(obtained.Category, Equals, "AUTHN")

	c.Check(obtained.String, Equals, "test string")
	c.Check(obtained.Duration, Equals, time.Duration(90*time.Second))
	c.Check(obtained.Timestamp, Equals, time.Date(2025, 10, 8, 8, 0, 0, 0, time.UTC))
	c.Check(obtained.Float64, Equals, float64(3.141592653589793))
	c.Check(obtained.Int64, Equals, int64(-4611686018427387904))
	c.Check(obtained.Int, Equals, int(-4294967296))
	c.Check(obtained.Uint64, Equals, uint64(4294967295))
	c.Check(obtained.Any, DeepEquals, map[string]any{"k": "v", "n": float64(1)})
}

func (s *SlogSuite) TestLogLoginSuccess(c *C) {
	logger := s.provider.New(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	type LoginSuccess struct {
		baseAttrs
		Event string `json:"event"`
		User  struct {
			ID             int64  `json:"snapd-user-id"`
			SystemUserName string `json:"system-user-name"`
			StoreUserEmail string `json:"store-user-email"`
			Expiration     string `json:"expiration"`
		} `json:"user"`
	}

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@gmail.com",
		SystemUserName: "jdoe",
	}
	logger.LogLoginSuccess(user)

	var obtained LoginSuccess
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "INFO")
	c.Check(obtained.Description, Equals, "User 42:user@gmail.com:jdoe login success")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Event, Equals, "authn_login_success")
	c.Check(obtained.User.ID, Equals, int64(42))
	c.Check(obtained.User.StoreUserEmail, Equals, "user@gmail.com")
	c.Check(obtained.User.SystemUserName, Equals, "jdoe")

	// verify key order for human readability
	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event", "user",
	})
}

func (s *SlogSuite) TestLogLoginFailure(c *C) {
	logger := s.provider.New(s.buf, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	type loginFailure struct {
		baseAttrs
		Event string `json:"event"`
		User  struct {
			ID             int64  `json:"snapd-user-id"`
			SystemUserName string `json:"system-user-name"`
			StoreUserEmail string `json:"store-user-email"`
			Expiration     string `json:"expiration"`
		} `json:"user"`
	}

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@gmail.com",
		SystemUserName: "jdoe",
	}
	logger.LogLoginFailure(user)

	var obtained loginFailure
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(time.Since(obtained.Datetime) < time.Second, Equals, true)
	c.Check(obtained.Level, Equals, "WARN")
	c.Check(obtained.Description, Equals, "User 42:user@gmail.com:jdoe login failure")
	c.Check(obtained.AppID, Equals, s.appID)
	c.Check(obtained.Event, Equals, "authn_login_failure")
	c.Check(obtained.User.ID, Equals, int64(42))
	c.Check(obtained.User.StoreUserEmail, Equals, "user@gmail.com")
	c.Check(obtained.User.SystemUserName, Equals, "jdoe")

	// verify key order for human readability
	keys, err := orderedKeys(s.buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(keys, DeepEquals, []string{
		"datetime", "level", "description",
		"app_id", "type", "category", "event", "user",
	})
}

func (s *SlogSuite) TestLevelWriterSink(c *C) {
	// wrap buffer in a journalWriter to exercise the levelWriter
	// branch in newSlogLogger and the levelHandler wrapper
	jw := seclog.NewJournalWriter(s.buf)
	logger := s.provider.New(jw, s.appID, seclog.LevelInfo)
	c.Assert(logger, NotNil)

	admin := seclog.SnapdUser{
		ID:             1,
		SystemUserName: "admin",
	}
	logger.LogLoginSuccess(admin)

	// the journalWriter prepends a syslog priority prefix
	raw := s.buf.String()
	// INFO maps to syslog.LOG_INFO (6)
	c.Check(raw[:3], Equals, "<6>")

	// the JSON payload follows the prefix
	var obtained map[string]any
	err := json.Unmarshal([]byte(raw[3:]), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["level"], Equals, "INFO")
	c.Check(obtained["event"], Equals, "authn_login_success")
	userMap, ok := obtained["user"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(userMap["system-user-name"], Equals, "admin")

	// log a WARN-level message and verify the prefix changes
	s.buf.Reset()
	logger.LogLoginFailure(admin)

	raw = s.buf.String()
	// WARN maps to syslog.LOG_WARNING (4)
	c.Check(raw[:3], Equals, "<4>")
}
