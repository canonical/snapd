// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2025 Canonical Ltd
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

package secboot_test

import (
	"context"

	"github.com/snapcore/secboot/efi/preinstall"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type preinstallSuite struct {
	testutil.BaseTest
}

var _ = Suite(&preinstallSuite{})

func (s *preinstallSuite) SetUpTest(c *C) {
}

func (s *preinstallSuite) TestConvertErrorType(c *C) {
}

func (s *preinstallSuite) TestConvertActions(c *C) {
}

func (s *preinstallSuite) TestNewInternalErrorUnexpectedType(c *C) {
}

func (s *preinstallSuite) TestPreinstallCheckHappy(c *C) {
	restore := secboot.MockSbPreinstallRun(func(checkCtx *preinstall.RunChecksContext, ctx context.Context, action preinstall.Action, args ...any) (*preinstall.CheckResult, error) {
		c.Assert(checkCtx, NotNil)
		c.Check(checkCtx.Errors(), IsNil)
		c.Check(checkCtx.Result(), IsNil)
		c.Assert(ctx, NotNil)
		c.Assert(action, Equals, preinstall.ActionNone)
		c.Assert(args, HasLen, 0)

		return &preinstall.CheckResult{}, nil
	})
	defer restore()

	err := secboot.PreinstallCheck(secboot.TPMProvisionFull)
	c.Assert(err, IsNil)
}

func (s *preinstallSuite) TestPreinstallCheckErrors(c *C) {
}

func (s *preinstallSuite) TestUnpackPreinstallCheckError(c *C) {
}
