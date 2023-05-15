// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
package boot_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

// TODO:UC20: move this to bootloadertest package and use from i.e. managers_test.go ?
func runBootloaderLogic(c *C, bl bootloader.Bootloader) (snap.PlaceInfo, error) {
	// switch on which kind of bootloader we have
	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if ok {
		return extractedRunKernelImageBootloaderLogic(c, ebl)
	}

	return pureenvBootloaderLogic(c, "kernel_status", bl)
}

// runBootloaderLogic implements the logic from the gadget snap bootloader,
// namely that we transition kernel_status "try" -> "trying" and "trying" -> ""
// and use try-kernel.efi when kernel_status is "try" and kernel.efi in all
// other situations
func extractedRunKernelImageBootloaderLogic(c *C, ebl bootloader.ExtractedRunKernelImageBootloader) (snap.PlaceInfo, error) {
	m, err := ebl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	kernStatus := m["kernel_status"]

	kern, err := ebl.Kernel()
	c.Assert(err, IsNil)
	c.Assert(kern, Not(IsNil))

	switch kernStatus {
	case boot.DefaultStatus:
	case boot.TryStatus:
		// move to trying, use the try-kernel
		m["kernel_status"] = boot.TryingStatus

		// ensure that the try-kernel exists
		tryKern, err := ebl.TryKernel()
		c.Assert(err, IsNil)
		c.Assert(tryKern, Not(IsNil))
		kern = tryKern

	case boot.TryingStatus:
		// boot failed, move back to default
		m["kernel_status"] = boot.DefaultStatus
	}

	err = ebl.SetBootVars(m)
	c.Assert(err, IsNil)

	return kern, nil
}

func pureenvBootloaderLogic(c *C, modeVar string, bl bootloader.Bootloader) (snap.PlaceInfo, error) {
	m, err := bl.GetBootVars(modeVar, "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	var kern snap.PlaceInfo

	kernStatus := m[modeVar]

	kern, err = snap.ParsePlaceInfoFromSnapFileName(m["snap_kernel"])
	c.Assert(err, IsNil)
	c.Assert(kern, Not(IsNil))

	switch kernStatus {
	case boot.DefaultStatus:
		// nothing to do, use normal kernel

	case boot.TryStatus:
		// move to trying, use the try-kernel
		m[modeVar] = boot.TryingStatus

		tryKern, err := snap.ParsePlaceInfoFromSnapFileName(m["snap_try_kernel"])
		c.Assert(err, IsNil)
		c.Assert(tryKern, Not(IsNil))
		kern = tryKern

	case boot.TryingStatus:
		// boot failed, move back to default status
		m[modeVar] = boot.DefaultStatus

	}

	err = bl.SetBootVars(m)
	c.Assert(err, IsNil)

	return kern, nil
}

// note: this could be implemented just as a function which takes a bootloader
// as an argument and then inspect the type of MockBootloader that was passed
// in, but the gains are little, since we don't need to use this function for
// the non-ExtractedRunKernelImageBootloader implementations, as those
// implementations just have one critical function to run which is just
// SetBootVars
func (s *bootenv20Suite) checkBootStateAfterUnexpectedRebootAndCleanup(
	c *C,
	dev snap.Device,
	bootFunc func(snap.Device) error,
	panicFunc string,
	expectedBootedKernel snap.PlaceInfo,
	expectedModeenvCurrentKernels []snap.PlaceInfo,
	blKernelAfterReboot snap.PlaceInfo,
	comment string,
) {
	if panicFunc != "" {
		// setup a panic during the given bootloader function
		restoreBootloaderPanic := s.bootloader.SetMockToPanic(panicFunc)

		// run the boot function that will now panic
		c.Assert(
			func() { bootFunc(dev) },
			PanicMatches,
			fmt.Sprintf("mocked reboot panic in %s", panicFunc),
			Commentf(comment),
		)

		// don't panic anymore
		restoreBootloaderPanic()
	} else {
		// just run the function directly
		err := bootFunc(dev)
		c.Assert(err, IsNil, Commentf(comment))
	}

	// do the bootloader kernel failover logic handling
	nextBootingKernel, err := runBootloaderLogic(c, s.bootloader)
	c.Assert(err, IsNil, Commentf(comment))

	// check that the kernel we booted now is expected
	c.Assert(nextBootingKernel, Equals, expectedBootedKernel, Commentf(comment))

	// also check that the normal kernel on the bootloader is what we expect
	kern, err := s.bootloader.Kernel()
	c.Assert(err, IsNil, Commentf(comment))
	c.Assert(kern, Equals, blKernelAfterReboot, Commentf(comment))

	// mark the boot successful like we were rebooted
	err = boot.MarkBootSuccessful(dev)
	c.Assert(err, IsNil, Commentf(comment))

	// the boot vars should be empty now too
	afterVars, err := s.bootloader.GetBootVars("kernel_status")
	c.Assert(err, IsNil, Commentf(comment))
	c.Assert(afterVars["kernel_status"], DeepEquals, boot.DefaultStatus, Commentf(comment))

	// the modeenv's setting for CurrentKernels also matches
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil, Commentf(comment))
	// it's nicer to pass in just the snap.PlaceInfo's, but to compare we need
	// the string filenames
	currentKernels := make([]string, len(expectedModeenvCurrentKernels))
	for i, sn := range expectedModeenvCurrentKernels {
		currentKernels[i] = sn.Filename()
	}
	c.Assert(m.CurrentKernels, DeepEquals, currentKernels, Commentf(comment))

	// the final kernel on the bootloader should always match what we booted -
	// after MarkSuccessful runs that is
	afterKernel, err := s.bootloader.Kernel()
	c.Assert(err, IsNil, Commentf(comment))
	c.Assert(afterKernel, DeepEquals, expectedBootedKernel, Commentf(comment))

	// we should never have a leftover try kernel
	_, err = s.bootloader.TryKernel()
	c.Assert(err, Equals, bootloader.ErrNoTryKernelRef, Commentf(comment))
}

func (s *bootenv20Suite) TestHappyMarkBootSuccessful20KernelUpgradeUnexpectedReboots(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	tt := []struct {
		rebootBeforeFunc  string
		expBootKernel     snap.PlaceInfo
		expModeenvKernels []snap.PlaceInfo
		expBlKernel       snap.PlaceInfo
		comment           string
	}{
		{
			"",                        // don't do any reboots for the happy path
			s.kern2,                   // we should boot the new kernel
			[]snap.PlaceInfo{s.kern2}, // expected modeenv kernel is new one
			s.kern2,                   // after reboot, current kernel on bl is new one
			"happy path",
		},
		{
			"SetBootVars",             // reboot right before SetBootVars
			s.kern1,                   // we should boot the old kernel
			[]snap.PlaceInfo{s.kern1}, // expected modeenv kernel is old one
			s.kern1,                   // after reboot, current kernel on bl is old one
			"reboot before SetBootVars results in old kernel",
		},
		{
			"EnableKernel",            // reboot right before EnableKernel
			s.kern1,                   // we should boot the old kernel
			[]snap.PlaceInfo{s.kern1}, // expected modeenv kernel is old one
			s.kern1,                   // after reboot, current kernel on bl is old one
			"reboot before EnableKernel results in old kernel",
		},
		{
			"DisableTryKernel",        // reboot right before DisableTryKernel
			s.kern2,                   // we should boot the new kernel
			[]snap.PlaceInfo{s.kern2}, // expected modeenv kernel is new one
			s.kern2,                   // after reboot, current kernel on bl is new one
			"reboot before DisableTryKernel results in new kernel",
		},
	}

	for _, t := range tt {
		// setup the bootloader per test
		restore := setupUC20Bootenv(
			c,
			s.bootloader,
			s.normalTryingKernelState,
		)

		s.checkBootStateAfterUnexpectedRebootAndCleanup(
			c,
			coreDev,
			boot.MarkBootSuccessful,
			t.rebootBeforeFunc,
			t.expBlKernel,
			t.expModeenvKernels,
			t.expBlKernel,
			t.comment,
		)

		restore()
	}
}

func (s *bootenv20Suite) TestHappySetNextBoot20KernelUpgradeUnexpectedReboots(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	tt := []struct {
		rebootBeforeFunc  string
		expBootKernel     snap.PlaceInfo
		expModeenvKernels []snap.PlaceInfo
		expBlKernel       snap.PlaceInfo
		comment           string
	}{
		{
			"",                        // don't do any reboots for the happy path
			s.kern2,                   // we should boot the new kernel
			[]snap.PlaceInfo{s.kern2}, // final expected modeenv kernel is new one
			s.kern1,                   // after reboot, current kernel on bl is old one
			"happy path",
		},
		{
			"EnableTryKernel",         // reboot right before EnableTryKernel
			s.kern1,                   // we should boot the old kernel
			[]snap.PlaceInfo{s.kern1}, // final expected modeenv kernel is old one
			s.kern1,                   // after reboot, current kernel on bl is old one
			"reboot before EnableTryKernel results in old kernel",
		},
		{
			"SetBootVars",             // reboot right before SetBootVars
			s.kern1,                   // we should boot the old kernel
			[]snap.PlaceInfo{s.kern1}, // final expected modeenv kernel is old one
			s.kern1,                   // after reboot, current kernel on bl is old one
			"reboot before SetBootVars results in old kernel",
		},
	}

	for _, t := range tt {
		// setup the bootloader per test
		restore := setupUC20Bootenv(
			c,
			s.bootloader,
			s.normalDefaultState,
		)

		// get the boot kernel participant from our new kernel snap
		bootKern := boot.Participant(s.kern2, snap.TypeKernel, coreDev)
		// make sure it's not a trivial boot participant
		c.Assert(bootKern.IsTrivial(), Equals, false)

		setNextFunc := func(snap.Device) error {
			// we don't care about the reboot required logic here
			_, err := bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false})
			return err
		}

		s.checkBootStateAfterUnexpectedRebootAndCleanup(
			c,
			coreDev,
			setNextFunc,
			t.rebootBeforeFunc,
			t.expBootKernel,
			t.expModeenvKernels,
			t.expBlKernel,
			t.comment,
		)

		restore()
	}
}
