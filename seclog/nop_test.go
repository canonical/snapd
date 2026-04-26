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

func (s *NopSuite) TestLogLoggingEnabled(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogLoggingEnabled()
}

func (s *NopSuite) TestLogLoggingDisabled(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogLoggingDisabled()
}

func (s *NopSuite) TestLogLoginSuccess(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogLoginSuccess(seclog.SnapdUser{StoreUserEmail: "user@gmail.com"})
}

func (s *NopSuite) TestLogLoginFailure(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogLoginFailure(seclog.SnapdUser{StoreUserEmail: "user@gmail.com"}, seclog.Reason{})
}

func (s *NopSuite) TestLogUserCreated(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogUserCreated(seclog.SnapdUser{StoreUserEmail: "user@gmail.com"})
}

func (s *NopSuite) TestLogUserUpdated(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogUserUpdated(
		seclog.SnapdUser{StoreUserEmail: "user@gmail.com"},
		[]string{"email"},
	)
}

func (s *NopSuite) TestLogUserRemoved(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogUserRemoved(seclog.SnapdUser{StoreUserEmail: "user@gmail.com"})
}

func (s *NopSuite) TestLogSystemUserCreated(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogSystemUserCreated(
		seclog.SystemUser{SystemUserName: "jdoe"},
		seclog.AddOptions{Gecos: "John Doe", Sudoer: true},
	)
}

func (s *NopSuite) TestLogSystemUserRemoved(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all messages without error
	logger.LogSystemUserRemoved(
		seclog.SystemUser{SystemUserName: "jdoe"},
		seclog.RemoveOptions{Force: true},
	)
}
