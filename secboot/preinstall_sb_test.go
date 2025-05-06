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
	"encoding/json"
	"fmt"

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
	jsonArgs, err := json.Marshal(struct {
		arg1 string
		arg2 string
	}{
		arg1: "arg1",
		arg2: "arg2",
	})
	c.Assert(err, IsNil)

	errorAndAction := preinstall.ErrorKindAndActions{
		ErrorKind: preinstall.ErrorKindRebootRequired,
		ErrorArgs: jsonArgs,
		Actions:   []preinstall.Action{preinstall.ActionReboot, preinstall.ActionShutdown},
	}

	//XXX: Need secboot helper for constructing errors
	var convertedErr secboot.PreinstallErrorAndActions
	c.Assert(func() { convertedErr = secboot.ConvertErrorType(&errorAndAction) }, PanicMatches, "runtime error: invalid memory address or nil pointer dereference")
	c.Assert(convertedErr, DeepEquals, secboot.PreinstallErrorAndActions{})
}

func (s *preinstallSuite) TestConvertActions(c *C) {
	testCases := []struct {
		actions  []preinstall.Action
		expected []string
	}{
		{nil, []string{}},
		{[]preinstall.Action{}, []string{}},
		{[]preinstall.Action{preinstall.ActionReboot, preinstall.ActionShutdown}, []string{"reboot", "shutdown"}},
	}

	for i, tc := range testCases {
		convertedActions := secboot.ConvertActions(tc.actions)
		c.Check(convertedActions, DeepEquals, tc.expected, Commentf("test case %d failed", i))
	}
}

func (s *preinstallSuite) TestNewInternalErrorUnexpectedType(c *C) {
	testCases := []struct {
		err      error
		expected secboot.PreinstallErrorAndActions
	}{
		{nil, secboot.PreinstallErrorAndActions{
			Kind:    "internal-error",
			Message: "cannot convert error of unexpected type <nil> (<nil>)"},
		},
		{fmt.Errorf("error message"), secboot.PreinstallErrorAndActions{
			Kind:    "internal-error",
			Message: "cannot convert error of unexpected type *errors.errorString (error message)",
		}},
	}

	for i, tc := range testCases {
		errorAndActions := secboot.NewInternalErrorUnexpectedType(tc.err)
		c.Check(errorAndActions, DeepEquals, tc.expected, Commentf("test case %d failed", i))
	}
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
