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
	"errors"

	"github.com/canonical/go-tpm2"
	"github.com/snapcore/secboot/efi"
	"github.com/snapcore/secboot/efi/preinstall"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

type preinstallSuite struct {
	testutil.BaseTest
}

var _ = Suite(&preinstallSuite{})

func (s *preinstallSuite) SetUpTest(c *C) {
}

func (s *preinstallSuite) TestConvertPreinstallCheckErrorActions(c *C) {
	testCases := []struct {
		actions  []preinstall.Action
		expected []string
	}{
		{nil, nil},
		{[]preinstall.Action{}, []string{}},
		{[]preinstall.Action{preinstall.ActionReboot, preinstall.ActionShutdown}, []string{"reboot", "shutdown"}},
	}

	for _, tc := range testCases {
		convertedActions := secboot.ConvertPreinstallCheckErrorActions(tc.actions)
		c.Check(convertedActions, DeepEquals, tc.expected)
	}
}

func (s *preinstallSuite) TestConvertPreinstallCheckErrorType(c *C) {
	kindAndActionsErr := preinstall.NewWithKindAndActionsError(
		preinstall.ErrorKindTPMHierarchiesOwned,
		preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}, WithAuthPolicy: tpm2.HandleList{tpm2.HandleOwner}},
		[]preinstall.Action{preinstall.ActionRebootToFWSettings},
		errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
	)

	var errorInfo secboot.PreinstallErrorInfo
	c.Assert(func() { errorInfo = secboot.ConvertPreinstallCheckErrorType(nil) }, PanicMatches, "runtime error: invalid memory address or nil pointer dereference")
	errorInfo = secboot.ConvertPreinstallCheckErrorType(kindAndActionsErr)
	c.Assert(errorInfo, DeepEquals, secboot.PreinstallErrorInfo{
		Kind:    "tpm-hierarchies-owned",
		Message: "error with TPM2 device: one or more of the TPM hierarchies is already owned",
		Args: map[string]json.RawMessage{
			"with-auth-value":  json.RawMessage(`[1073741834]`),
			"with-auth-policy": json.RawMessage(`[1073741825]`),
		},
		Actions: []string{"reboot-to-fw-settings"},
	})
}

func (s *preinstallSuite) TestUnpackPreinstallCheckErrorCompound(c *C) {
	compoundError := &secboot.CompoundPreinstallCheckError{
		[]error{
			preinstall.NewWithKindAndActionsError(
				preinstall.ErrorKindTPMHierarchiesOwned,
				preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}},
				[]preinstall.Action{preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
			),
			preinstall.NewWithKindAndActionsError(
				preinstall.ErrorKindTPMDeviceLockout,
				&preinstall.TPMDeviceLockoutArgs{IntervalDuration: 7200000000000, TotalDuration: 230400000000000},
				[]preinstall.Action{preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: TPM is in DA lockout mode"),
			),
			preinstall.NewWithKindAndActionsError(
				preinstall.ErrorKindNone,
				nil,
				nil,
				nil,
			),
		},
	}

	errorInfos, err := secboot.UnpackPreinstallCheckError(compoundError)
	c.Assert(err, IsNil)
	c.Assert(errorInfos, DeepEquals, []secboot.PreinstallErrorInfo{
		{
			Kind:    "tpm-hierarchies-owned",
			Message: "error with TPM2 device: one or more of the TPM hierarchies is already owned",
			Args: map[string]json.RawMessage{
				"with-auth-value":  json.RawMessage(`[1073741834]`),
				"with-auth-policy": json.RawMessage(`null`),
			},
			Actions: []string{"reboot-to-fw-settings"},
		},
		{
			Kind:    "tpm-device-lockout",
			Message: "error with TPM2 device: TPM is in DA lockout mode",
			Args: map[string]json.RawMessage{
				"interval-duration": json.RawMessage(`7200000000000`),
				"total-duration":    json.RawMessage(`230400000000000`),
			},
			Actions: []string{"reboot-to-fw-settings"},
		},
		{
			Kind:    "",
			Message: "<nil>",
			Args:    nil,
			Actions: nil,
		},
	})

	jsn, err := json.MarshalIndent(errorInfos, "", "  ")
	c.Assert(err, IsNil)
	const expectedJson = `[
  {
    "kind": "tpm-hierarchies-owned",
    "message": "error with TPM2 device: one or more of the TPM hierarchies is already owned",
    "args": {
      "with-auth-policy": null,
      "with-auth-value": [
        1073741834
      ]
    },
    "actions": [
      "reboot-to-fw-settings"
    ]
  },
  {
    "kind": "tpm-device-lockout",
    "message": "error with TPM2 device: TPM is in DA lockout mode",
    "args": {
      "interval-duration": 7200000000000,
      "total-duration": 230400000000000
    },
    "actions": [
      "reboot-to-fw-settings"
    ]
  },
  {
    "kind": "",
    "message": "\u003cnil\u003e"
  }
]`
	c.Assert(string(jsn), Equals, expectedJson)
}

func (s *preinstallSuite) TestUnpackPreinstallCheckErrorFailCompoundUnexpectedType(c *C) {
	compoundError := &secboot.CompoundPreinstallCheckError{
		[]error{
			preinstall.NewWithKindAndActionsError(
				preinstall.ErrorKindTPMHierarchiesOwned,
				preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}},
				[]preinstall.Action{preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
			),
			preinstall.ErrInsufficientDMAProtection,
		},
	}

	errorInfos, err := secboot.UnpackPreinstallCheckError(compoundError)
	c.Assert(err, ErrorMatches, `cannot unpack error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
	c.Assert(errorInfos, IsNil)
}

func (s *preinstallSuite) TestUnpackPreinstallCheckErrorFailCompoundWrapsNil(c *C) {
	compoundError := &secboot.CompoundPreinstallCheckError{nil}

	errorInfos, err := secboot.UnpackPreinstallCheckError(compoundError)
	c.Assert(err, ErrorMatches, "compound error does not wrap any error")
	c.Assert(errorInfos, IsNil)
}

func (s *preinstallSuite) TestUnpackPreinstallCheckErrorSingle(c *C) {
	singleError := preinstall.NewWithKindAndActionsError(
		preinstall.ErrorKindTPMDeviceDisabled,
		nil,
		[]preinstall.Action{preinstall.ActionRebootToFWSettings},
		errors.New("error with TPM2 device: TPM2 device is present but is currently disabled by the platform firmware"),
	)

	errorInfos, err := secboot.UnpackPreinstallCheckError(singleError)
	c.Assert(err, IsNil)
	c.Assert(errorInfos, DeepEquals, []secboot.PreinstallErrorInfo{
		{
			Kind:    "tpm-device-disabled",
			Message: "error with TPM2 device: TPM2 device is present but is currently disabled by the platform firmware",
			Actions: []string{"reboot-to-fw-settings"},
		},
	})

	jsn, err := json.MarshalIndent(errorInfos, "", "  ")
	c.Assert(err, IsNil)
	const expectedJson = `[
  {
    "kind": "tpm-device-disabled",
    "message": "error with TPM2 device: TPM2 device is present but is currently disabled by the platform firmware",
    "actions": [
      "reboot-to-fw-settings"
    ]
  }
]`
	c.Assert(string(jsn), Equals, expectedJson)
}

func (s *preinstallSuite) TestUnpackPreinstallCheckErrorFailSingleUnexpectedType(c *C) {
	errorInfos, err := secboot.UnpackPreinstallCheckError(preinstall.ErrInsufficientDMAProtection)
	c.Assert(err, ErrorMatches, `cannot unpack error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
	c.Assert(errorInfos, IsNil)
}

func (s *preinstallSuite) testPreinstallCheckConfig(c *C, isTesting, isVM, permitVM bool) {
	restore := snapdenv.MockTesting(isTesting)
	defer restore()
	cmdExit := `exit 1`
	if isVM {
		cmdExit = `exit 0`
	}
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", cmdExit)
	defer systemdCmd.Restore()

	restore = secboot.MockSbPreinstallNewRunChecksContext(
		func(initialFlags preinstall.CheckFlags, loadedImages []efi.Image, profileOpts preinstall.PCRProfileOptionsFlags) *preinstall.RunChecksContext {
			if permitVM {
				c.Assert(initialFlags&preinstall.PermitVirtualMachine, Equals, preinstall.PermitVirtualMachine)
			} else {
				c.Assert(initialFlags, Equals, preinstall.CheckFlagsDefault)
			}
			c.Assert(profileOpts, Equals, preinstall.PCRProfileOptionsDefault)
			c.Assert(loadedImages, IsNil)

			return nil
		})
	defer restore()

	restore = secboot.MockSbPreinstallRun(
		func(checkCtx *preinstall.RunChecksContext, ctx context.Context, action preinstall.Action, args ...any) (*preinstall.CheckResult, error) {
			c.Assert(checkCtx, IsNil)
			c.Assert(ctx, NotNil)
			c.Assert(action, Equals, preinstall.ActionNone)
			c.Assert(args, IsNil)

			return &preinstall.CheckResult{}, nil
		})
	defer restore()

	errorInfos, err := secboot.PreinstallCheck(nil)
	c.Assert(err, IsNil)
	c.Assert(errorInfos, IsNil)
}

func (s *preinstallSuite) TestPreinstallCheckConfig(c *C) {
	testCases := []struct {
		isTesting bool
		isVM      bool
		permitVM  bool
	}{
		{false, false, false}, // default config
		{false, true, false},  // default config
		{true, false, false},  // default config
		{true, true, true},    // modify default config to permit VM
	}

	for _, tc := range testCases {
		s.testPreinstallCheckConfig(c, tc.isTesting, tc.isVM, tc.permitVM)
	}
}

func (s *preinstallSuite) testPreinstallCheck(c *C, detectErrors, failUnpack bool) {
	bootImagePaths := []string{
		"/cdrom/EFI/boot/boot*.efi",
		"/cdrom/EFI/boot/grub*.efi",
		"/cdrom/casper/vmlinuz",
	}

	restore := snapdenv.MockTesting(false)
	defer restore()

	restore = secboot.MockSbPreinstallNewRunChecksContext(
		func(initialFlags preinstall.CheckFlags, loadedImages []efi.Image, profileOpts preinstall.PCRProfileOptionsFlags) *preinstall.RunChecksContext {
			c.Assert(initialFlags, Equals, preinstall.CheckFlagsDefault)
			c.Assert(profileOpts, Equals, preinstall.PCRProfileOptionsDefault)
			c.Assert(loadedImages, HasLen, len(bootImagePaths))
			for i, image := range loadedImages {
				c.Check(image.String(), Equals, bootImagePaths[i])
			}

			return &preinstall.RunChecksContext{}
		})
	defer restore()

	restore = secboot.MockSbPreinstallRun(
		func(checkCtx *preinstall.RunChecksContext, ctx context.Context, action preinstall.Action, args ...any) (*preinstall.CheckResult, error) {
			c.Assert(checkCtx, NotNil)
			c.Assert(checkCtx.Errors(), IsNil)
			c.Assert(checkCtx.Result(), IsNil)
			c.Assert(ctx, NotNil)
			c.Assert(action, Equals, preinstall.ActionNone)
			c.Assert(args, IsNil)

			if detectErrors {
				return nil, &secboot.CompoundPreinstallCheckError{
					[]error{
						preinstall.NewWithKindAndActionsError(
							preinstall.ErrorKindTPMHierarchiesOwned,
							preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout},
								WithAuthPolicy: tpm2.HandleList{tpm2.HandleOwner}},
							[]preinstall.Action{preinstall.ActionRebootToFWSettings},
							errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
						),
						preinstall.NewWithKindAndActionsError(
							preinstall.ErrorKindTPMDeviceLockout,
							&preinstall.TPMDeviceLockoutArgs{IntervalDuration: 7200000000000, TotalDuration: 230400000000000},
							[]preinstall.Action{preinstall.ActionRebootToFWSettings},
							errors.New("error with TPM2 device: TPM is in DA lockout mode"),
						),
					},
				}
			} else if failUnpack {
				return nil, preinstall.ErrInsufficientDMAProtection
			} else {
				return &preinstall.CheckResult{
					Warnings: &secboot.CompoundPreinstallCheckError{
						[]error{
							errors.New("warning 1"),
							errors.New("warning 2"),
						},
					},
				}, nil
			}
		})
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	errorInfos, err := secboot.PreinstallCheck(bootImagePaths)
	if detectErrors {
		c.Assert(err, IsNil)
		c.Assert(logbuf.String(), Equals, "")
		c.Assert(errorInfos, DeepEquals, []secboot.PreinstallErrorInfo{
			{
				Kind:    "tpm-hierarchies-owned",
				Message: "error with TPM2 device: one or more of the TPM hierarchies is already owned",
				Args: map[string]json.RawMessage{
					"with-auth-value":  json.RawMessage(`[1073741834]`),
					"with-auth-policy": json.RawMessage(`[1073741825]`),
				},
				Actions: []string{"reboot-to-fw-settings"},
			},
			{
				Kind:    "tpm-device-lockout",
				Message: "error with TPM2 device: TPM is in DA lockout mode",
				Args: map[string]json.RawMessage{
					"interval-duration": json.RawMessage(`7200000000000`),
					"total-duration":    json.RawMessage(`230400000000000`),
				},
				Actions: []string{"reboot-to-fw-settings"},
			},
		})
	} else if failUnpack {
		c.Assert(err, ErrorMatches, `cannot unpack error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
		c.Assert(errorInfos, IsNil)
	} else {
		c.Assert(err, IsNil)
		c.Assert(errorInfos, IsNil)
		c.Assert(logbuf.String(), testutil.Contains, "preinstall check warning: warning 1")
		c.Assert(logbuf.String(), testutil.Contains, "preinstall check warning: warning 2")
	}
}

func (s *preinstallSuite) TestPreinstallCheckWithWarningsAndErrors(c *C) {
	detectErrors := false // warnings and no errors
	s.testPreinstallCheck(c, detectErrors, false)
	detectErrors = true // errors and no warnings
	s.testPreinstallCheck(c, detectErrors, false)
}

func (s *preinstallSuite) TestPreinstallCheckFailUnpack(c *C) {
	failUnpack := true
	s.testPreinstallCheck(c, false, failUnpack)
}
