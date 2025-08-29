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
	sb_efi "github.com/snapcore/secboot/efi"
	sb_preinstall "github.com/snapcore/secboot/efi/preinstall"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type preinstallSuite struct {
	testutil.BaseTest
}

var _ = Suite(&preinstallSuite{})

func (s *preinstallSuite) SetUpTest(c *C) {
}

// CompoundPreinstallCheckError implements sb_preinstall.CompoundError and is
// used to mimic compound errors that would normally be returned by secboot.
type CompoundPreinstallCheckError struct {
	Errs []error
}

func (e *CompoundPreinstallCheckError) Error() string {
	return "n/a"
}

func (e *CompoundPreinstallCheckError) Unwrap() []error {
	return e.Errs
}

func (s *preinstallSuite) TestConvertPreinstallCheckErrorActions(c *C) {
	testCases := []struct {
		actions  []sb_preinstall.Action
		expected []string
	}{
		{nil, nil},
		{[]sb_preinstall.Action{}, []string{}},
		{[]sb_preinstall.Action{sb_preinstall.ActionReboot, sb_preinstall.ActionShutdown}, []string{"reboot", "shutdown"}},
	}

	for _, tc := range testCases {
		convertedActions := secboot.ConvertPreinstallCheckErrorActions(tc.actions)
		c.Check(convertedActions, DeepEquals, tc.expected)
	}
}

func (s *preinstallSuite) TestConvertPreinstallCheckErrorType(c *C) {
	kindAndActionsErr := sb_preinstall.NewWithKindAndActionsError(
		sb_preinstall.ErrorKindTPMHierarchiesOwned,
		sb_preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}, WithAuthPolicy: tpm2.HandleList{tpm2.HandleOwner}},
		[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
		errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
	)

	var errorDetails secboot.PreinstallErrorDetails
	c.Assert(func() { errorDetails = secboot.ConvertPreinstallCheckErrorType(nil) }, PanicMatches, "runtime error: invalid memory address or nil pointer dereference")
	errorDetails = secboot.ConvertPreinstallCheckErrorType(kindAndActionsErr)
	c.Assert(errorDetails, DeepEquals, secboot.PreinstallErrorDetails{
		Kind:    "tpm-hierarchies-owned",
		Message: "error with TPM2 device: one or more of the TPM hierarchies is already owned",
		Args: map[string]json.RawMessage{
			"with-auth-value":  json.RawMessage(`[1073741834]`),
			"with-auth-policy": json.RawMessage(`[1073741825]`),
		},
		Actions: []string{"reboot-to-fw-settings"},
	})
}

func (s *preinstallSuite) TestUnwrapPreinstallCheckErrorCompound(c *C) {
	compoundError := &CompoundPreinstallCheckError{
		[]error{
			sb_preinstall.NewWithKindAndActionsError(
				sb_preinstall.ErrorKindTPMHierarchiesOwned,
				sb_preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}},
				[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
			),
			sb_preinstall.NewWithKindAndActionsError(
				sb_preinstall.ErrorKindTPMDeviceLockout,
				&sb_preinstall.TPMDeviceLockoutArgs{IntervalDuration: 7200000000000, TotalDuration: 230400000000000},
				[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: TPM is in DA lockout mode"),
			),
			sb_preinstall.NewWithKindAndActionsError(
				sb_preinstall.ErrorKindNone,
				nil,
				nil,
				nil,
			),
		},
	}

	errorDetails, err := secboot.UnwrapPreinstallCheckError(compoundError)
	c.Assert(err, IsNil)
	c.Assert(errorDetails, DeepEquals, []secboot.PreinstallErrorDetails{
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

	jsn, err := json.MarshalIndent(errorDetails, "", "  ")
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

func (s *preinstallSuite) TestUnwrapPreinstallCheckErrorFailCompoundUnexpectedType(c *C) {
	compoundError := &CompoundPreinstallCheckError{
		[]error{
			sb_preinstall.NewWithKindAndActionsError(
				sb_preinstall.ErrorKindTPMHierarchiesOwned,
				sb_preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout}},
				[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
				errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
			),
			sb_preinstall.ErrInsufficientDMAProtection,
		},
	}

	errorDetails, err := secboot.UnwrapPreinstallCheckError(compoundError)
	c.Assert(err, ErrorMatches, `cannot unwrap error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
	c.Assert(errorDetails, IsNil)
}

func (s *preinstallSuite) TestUnwrapPreinstallCheckErrorFailCompoundWrapsNil(c *C) {
	compoundError := &CompoundPreinstallCheckError{nil}

	errorDetails, err := secboot.UnwrapPreinstallCheckError(compoundError)
	c.Assert(err, ErrorMatches, "compound error does not wrap any error")
	c.Assert(errorDetails, IsNil)
}

func (s *preinstallSuite) TestUnwrapPreinstallCheckErrorSingle(c *C) {
	singleError := sb_preinstall.NewWithKindAndActionsError(
		sb_preinstall.ErrorKindTPMDeviceDisabled,
		nil,
		[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
		errors.New("error with TPM2 device: TPM2 device is present but is currently disabled by the platform firmware"),
	)

	errorDetails, err := secboot.UnwrapPreinstallCheckError(singleError)
	c.Assert(err, IsNil)
	c.Assert(errorDetails, DeepEquals, []secboot.PreinstallErrorDetails{
		{
			Kind:    "tpm-device-disabled",
			Message: "error with TPM2 device: TPM2 device is present but is currently disabled by the platform firmware",
			Actions: []string{"reboot-to-fw-settings"},
		},
	})

	jsn, err := json.MarshalIndent(errorDetails, "", "  ")
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

func (s *preinstallSuite) TestUnwrapPreinstallCheckErrorFailSingleUnexpectedType(c *C) {
	errorDetails, err := secboot.UnwrapPreinstallCheckError(sb_preinstall.ErrInsufficientDMAProtection)
	c.Assert(err, ErrorMatches, `cannot unwrap error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
	c.Assert(errorDetails, IsNil)
}

func (s *preinstallSuite) testPreinstallCheckConfig(c *C, isVM, permitVM bool) {
	cmdExit := `exit 1`
	if isVM {
		cmdExit = `exit 0`
	}
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", cmdExit)
	s.AddCleanup(systemdCmd.Restore)

	restore := secboot.MockSbPreinstallNewRunChecksContext(
		func(initialFlags sb_preinstall.CheckFlags, loadedImages []sb_efi.Image, profileOpts sb_preinstall.PCRProfileOptionsFlags) *sb_preinstall.RunChecksContext {
			if permitVM {
				c.Assert(initialFlags, Equals, sb_preinstall.PermitVirtualMachine|sb_preinstall.PermitVARSuppliedDrivers)
			} else {
				c.Assert(initialFlags, Equals, sb_preinstall.PermitVARSuppliedDrivers)
			}
			c.Assert(profileOpts, Equals, sb_preinstall.PCRProfileOptionsDefault)
			c.Assert(loadedImages, IsNil)

			return nil
		})
	s.AddCleanup(restore)

	restore = secboot.MockSbPreinstallRun(
		func(checkCtx *sb_preinstall.RunChecksContext, ctx context.Context, action sb_preinstall.Action, args ...any) (*sb_preinstall.CheckResult, error) {
			c.Assert(checkCtx, IsNil)
			c.Assert(ctx, NotNil)
			c.Assert(action, Equals, sb_preinstall.ActionNone)
			c.Assert(args, IsNil)

			return &sb_preinstall.CheckResult{}, nil
		})
	s.AddCleanup(restore)

	checkContext, errorDetails, err := secboot.PreinstallCheck(context.Background(), nil)
	c.Assert(checkContext, NotNil)
	c.Assert(err, IsNil)
	c.Assert(errorDetails, IsNil)
}

func (s *preinstallSuite) TestPreinstallCheckConfig(c *C) {
	testCases := []struct {
		isVM     bool
		permitVM bool
	}{
		{false, false}, // default config
		{true, true},   // modify default config to permit VM
	}

	for _, tc := range testCases {
		s.testPreinstallCheckConfig(c, tc.isVM, tc.permitVM)
	}
}

// testPreinstallCheckAndAction is a helper to test PreinstallCheck and PreinstallCheckAction
func (s *preinstallSuite) testPreinstallCheckAndAction(c *C, checkAction *secboot.PreinstallAction, detectErrors, failUnwrap bool) {
	bootImagePaths := []string{
		"/cdrom/EFI/boot/bootXXX.efi",
		"/cdrom/EFI/boot/grubXXX.efi",
		"/cdrom/casper/vmlinuz",
	}

	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", "exit 1")
	s.AddCleanup(systemdCmd.Restore)

	var expectedRunChecksContext *sb_preinstall.RunChecksContext

	restore := secboot.MockSbPreinstallNewRunChecksContext(
		func(initialFlags sb_preinstall.CheckFlags, loadedImages []sb_efi.Image, profileOpts sb_preinstall.PCRProfileOptionsFlags) *sb_preinstall.RunChecksContext {
			c.Assert(checkAction, IsNil)
			c.Assert(initialFlags, Equals, sb_preinstall.PermitVARSuppliedDrivers)
			c.Assert(profileOpts, Equals, sb_preinstall.PCRProfileOptionsDefault)
			c.Assert(loadedImages, HasLen, len(bootImagePaths))
			for i, image := range loadedImages {
				c.Check(image.String(), Equals, bootImagePaths[i])
			}

			return expectedRunChecksContext
		})
	s.AddCleanup(restore)

	var expectedAction sb_preinstall.Action

	restore = secboot.MockSbPreinstallRun(
		func(checkCtx *sb_preinstall.RunChecksContext, ctx context.Context, action sb_preinstall.Action, args ...any) (*sb_preinstall.CheckResult, error) {
			c.Assert(checkCtx, Equals, expectedRunChecksContext)
			c.Assert(checkCtx.Errors(), IsNil)
			c.Assert(checkCtx.Result(), IsNil)
			c.Assert(ctx, NotNil)
			c.Assert(action, Equals, expectedAction)
			c.Assert(args, IsNil)

			if detectErrors {
				return nil, &CompoundPreinstallCheckError{
					[]error{
						sb_preinstall.NewWithKindAndActionsError(
							sb_preinstall.ErrorKindTPMHierarchiesOwned,
							sb_preinstall.TPM2OwnedHierarchiesError{WithAuthValue: tpm2.HandleList{tpm2.HandleLockout},
								WithAuthPolicy: tpm2.HandleList{tpm2.HandleOwner}},
							[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
							errors.New("error with TPM2 device: one or more of the TPM hierarchies is already owned"),
						),
						sb_preinstall.NewWithKindAndActionsError(
							sb_preinstall.ErrorKindTPMDeviceLockout,
							&sb_preinstall.TPMDeviceLockoutArgs{IntervalDuration: 7200000000000, TotalDuration: 230400000000000},
							[]sb_preinstall.Action{sb_preinstall.ActionRebootToFWSettings},
							errors.New("error with TPM2 device: TPM is in DA lockout mode"),
						),
					},
				}
				// if we test with an action then only apply failure when called from PreinstallCheckAction
				// to ensure
			} else if failUnwrap {
				return nil, sb_preinstall.ErrInsufficientDMAProtection
			} else {
				return &sb_preinstall.CheckResult{
					Warnings: &CompoundPreinstallCheckError{
						[]error{
							errors.New("warning 1"),
							errors.New("warning 2"),
						},
					},
				}, nil
			}
		})
	s.AddCleanup(restore)

	logbuf, restore := logger.MockLogger()
	s.AddCleanup(restore)

	var (
		checkContext *secboot.PreinstallCheckContext
		errorDetails []secboot.PreinstallErrorDetails
		err          error
	)

	if checkAction == nil {
		// test PreinstallCheck
		expectedRunChecksContext = &sb_preinstall.RunChecksContext{}
		expectedAction = sb_preinstall.ActionNone
		checkContext, errorDetails, err = secboot.PreinstallCheck(context.Background(), bootImagePaths)
		if failUnwrap {
			c.Assert(checkContext, IsNil)
		} else {
			c.Assert(secboot.ExtractSbRunChecksContext(checkContext), Equals, expectedRunChecksContext)
		}
	} else {
		// test PreinstallCheckAction
		expectedRunChecksContext = &sb_preinstall.RunChecksContext{}
		checkContext = secboot.NewPreinstallChecksContext(expectedRunChecksContext)
		expectedAction = sb_preinstall.Action(checkAction.Action)
		errorDetails, err = checkContext.PreinstallCheckAction(context.Background(), checkAction)
	}

	// errorDetails and err should behave the same for PreinstallCheck and PreinstallCheckAction
	if detectErrors {
		c.Assert(err, IsNil)
		c.Assert(logbuf.String(), Equals, "")
		c.Assert(errorDetails, DeepEquals, []secboot.PreinstallErrorDetails{
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
	} else if failUnwrap {
		c.Assert(err, ErrorMatches, `cannot unwrap error of unexpected type \*errors\.errorString \(the platform firmware indicates that DMA protections are insufficient\)`)
		c.Assert(errorDetails, IsNil)
	} else {
		c.Assert(err, IsNil)
		c.Assert(errorDetails, IsNil)
		c.Assert(logbuf.String(), testutil.Contains, "preinstall check warning: warning 1")
		c.Assert(logbuf.String(), testutil.Contains, "preinstall check warning: warning 2")
	}
}

func (s *preinstallSuite) TestPreinstallCheckWithWarningsAndErrors(c *C) {
	detectErrors := false // warnings and no errors
	s.testPreinstallCheckAndAction(c, nil, detectErrors, false)

	detectErrors = true // errors and no warnings
	s.testPreinstallCheckAndAction(c, nil, detectErrors, false)
}

func (s *preinstallSuite) TestPreinstallCheckFailUnwrap(c *C) {
	failUnwrap := true
	s.testPreinstallCheckAndAction(c, nil, false, failUnwrap)
}

func (s *preinstallSuite) TestPreinstallCheckActionWithWarningsAndErrors(c *C) {
	action := &secboot.PreinstallAction{
		Action: string(sb_preinstall.ActionReboot),
	}
	detectErrors := false // warnings and no errors
	s.testPreinstallCheckAndAction(c, action, detectErrors, false)

	detectErrors = true // errors and no warnings
	s.testPreinstallCheckAndAction(c, action, detectErrors, false)
}

func (s *preinstallSuite) TestPreinstallActionFailUnwrap(c *C) {
	action := &secboot.PreinstallAction{
		Action: string(sb_preinstall.ActionReboot),
	}
	failUnwrap := true
	s.testPreinstallCheckAndAction(c, action, false, failUnwrap)
}

func (s *preinstallSuite) TestSaveCheckResultErrorNotAvailable(c *C) {
	checkContext := secboot.NewPreinstallChecksContext(&sb_preinstall.RunChecksContext{})
	err := checkContext.SaveCheckResult("preinstall-check-result")
	c.Assert(err, ErrorMatches, "preinstall check result unavailable: 0 unresolved errors")

	//TODO: extend test when there is a way to modify sb_preinstall.CheckResult within sb_preinstall.RunChecksContext
}
