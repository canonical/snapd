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
)

type NopSuite struct{}

var _ = Suite(&NopSuite{})

func (s *NopSuite) TestLogEventDiscards(c *C) {
	logger := seclog.NewNopLogger()
	c.Assert(logger, NotNil)

	// nop logger discards all events without error
	logger.LogEvent(
		seclog.Event{Category: "SYS", Name: "sys_logging_enabled", Level: seclog.LevelInfo},
		"Security logging enabled",
	)
	logger.LogEvent(
		seclog.Event{Category: "AUTHN", Name: "authn_login_failure", Level: seclog.LevelWarn},
		"test",
		seclog.Attr{Key: "user", Value: seclog.SnapdUser{StoreUserEmail: "user@gmail.com"}},
		seclog.Attr{Key: "error", Value: seclog.Reason{}},
	)
}
