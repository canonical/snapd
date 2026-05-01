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

package seclog_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

type NopSuite struct {
	testutil.BaseTest
}

var _ = Suite(&NopSuite{})

func (s *NopSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *NopSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *NopSuite) TestLogLoggerEnabled(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogAny(
		seclog.Event{Category: "SYS", Name: "sys_logging_enabled", Level: seclog.LevelInfo},
		"Security logging enabled",
	)
}

func (s *NopSuite) TestLogLoggerDisabled(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogAny(
		seclog.Event{Category: "SYS", Name: "sys_logging_disabled", Level: seclog.LevelCritical},
		"Security logging disabled",
	)
}

func (s *NopSuite) TestLogLoginSuccess(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogAny(
		seclog.Event{Category: "AUTHN", Name: "authn_login_success", Level: seclog.LevelInfo},
		"test",
		seclog.Attr{Key: "user", Value: seclog.SnapdUser{StoreUserEmail: "user@gmail.com"}},
	)
}

func (s *NopSuite) TestLogLoginFailure(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogAny(
		seclog.Event{Category: "AUTHN", Name: "authn_login_failure", Level: seclog.LevelWarn},
		"test",
		seclog.Attr{Key: "user", Value: seclog.SnapdUser{StoreUserEmail: "user@gmail.com"}},
		seclog.Attr{Key: "error", Value: seclog.Reason{}},
	)
}
