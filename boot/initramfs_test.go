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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

type initramfsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&initramfsSuite{})

func (s *initramfsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
}

func (s *initramfsSuite) TestEnsureNextBootToRunMode(c *C) {
	// with no bootloader available we can't mark successful
	err := boot.EnsureNextBootToRunMode("label")
	c.Assert(err, ErrorMatches, "cannot determine bootloader")

	// forcing a bootloader works
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	err = boot.EnsureNextBootToRunMode("label")
	c.Assert(err, IsNil)

	// the bloader vars have been updated
	m, err := bloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "label",
	})
}

func (s *initramfsSuite) TestEnsureNextBootToRunModeRealBootloader(c *C) {
	// create a real grub.cfg on ubuntu-seed
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu", "grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)

	err = boot.EnsureNextBootToRunMode("somelabel")
	c.Assert(err, IsNil)

	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	bloader, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, opts)
	c.Assert(err, IsNil)
	c.Assert(bloader.Name(), Equals, "grub")

	// the bloader vars have been updated
	m, err := bloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "somelabel",
	})
}

func makeSnapFilesOnInitramfsUbuntuData(c *C, comment CommentInterface, snaps ...snap.PlaceInfo) (restore func()) {
	// also make sure the snaps also exist on ubuntu-data
	snapDir := dirs.SnapBlobDirUnder(boot.InitramfsWritableDir)
	err := os.MkdirAll(snapDir, 0755)
	c.Assert(err, IsNil, comment)
	paths := make([]string, 0, len(snaps))
	for _, sn := range snaps {
		snPath := filepath.Join(snapDir, sn.Filename())
		paths = append(paths, snPath)
		err = ioutil.WriteFile(snPath, nil, 0644)
		c.Assert(err, IsNil, comment)
	}
	return func() {
		for _, path := range paths {
			err := os.Remove(path)
			c.Assert(err, IsNil, comment)
		}
	}
}

func (s *initramfsSuite) TestInitramfsRunModeSelectSnapsToMount(c *C) {
	// make some snap infos we will use in the tests
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)

	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	base1, err := snap.ParsePlaceInfoFromSnapFileName("core20_1.snap")
	c.Assert(err, IsNil)

	base2, err := snap.ParsePlaceInfoFromSnapFileName("core20_2.snap")
	c.Assert(err, IsNil)

	baseT := snap.TypeBase
	kernelT := snap.TypeKernel

	tt := []struct {
		m              *boot.Modeenv
		expectedM      *boot.Modeenv
		typs           []snap.Type
		kernel         snap.PlaceInfo
		trykernel      snap.PlaceInfo
		blvars         map[string]string
		snapsToMake    []snap.PlaceInfo
		expected       map[snap.Type]snap.PlaceInfo
		errPattern     string
		comment        string
		expRebootPanic string
	}{
		//
		// default paths
		//

		// default base path
		{
			m:           &boot.Modeenv{Mode: "run", Base: base1.Filename()},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base1},
			comment:     "default base path",
		},
		// default kernel path
		{
			m:           &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:      kernel1,
			typs:        []snap.Type{kernelT},
			snapsToMake: []snap.PlaceInfo{kernel1},
			expected:    map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			comment:     "default kernel path",
		},

		//
		// happy kernel upgrade paths
		//

		// kernel upgrade path
		{
			m:           &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()}},
			kernel:      kernel1,
			trykernel:   kernel2,
			typs:        []snap.Type{kernelT},
			blvars:      map[string]string{"kernel_status": boot.TryingStatus},
			snapsToMake: []snap.PlaceInfo{kernel1, kernel2},
			expected:    map[snap.Type]snap.PlaceInfo{kernelT: kernel2},
			comment:     "successful kernel upgrade path",
		},
		// extraneous kernel extracted/set, but kernel_status is default,
		// so the bootloader will ignore that and boot the default kernel
		// note that this test case is a bit ambiguous as we don't actually know
		// in the initramfs that the bootloader actually booted the default
		// kernel, we are just assuming that the bootloader implementation in
		// the real world is robust enough to only boot the try kernel if and
		// only if kernel_status is not DefaultStatus
		{
			m:           &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()}},
			kernel:      kernel1,
			trykernel:   kernel2,
			typs:        []snap.Type{kernelT},
			blvars:      map[string]string{"kernel_status": boot.DefaultStatus},
			snapsToMake: []snap.PlaceInfo{kernel1, kernel2},
			expected:    map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			comment:     "fallback kernel upgrade path, due to kernel_status empty (default)",
		},

		//
		// unhappy reboot fallback kernel paths
		//

		// kernel upgrade path, but reboots to fallback due to untrusted kernel from modeenv
		{
			m:              &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:         kernel1,
			trykernel:      kernel2,
			typs:           []snap.Type{kernelT},
			blvars:         map[string]string{"kernel_status": boot.TryingStatus},
			snapsToMake:    []snap.PlaceInfo{kernel1, kernel2},
			expRebootPanic: "reboot due to modeenv untrusted try kernel",
			comment:        "fallback kernel upgrade path, due to modeenv untrusted try kernel",
		},
		// kernel upgrade path, but reboots to fallback due to try kernel file not existing
		{
			m:              &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()}},
			kernel:         kernel1,
			trykernel:      kernel2,
			typs:           []snap.Type{kernelT},
			blvars:         map[string]string{"kernel_status": boot.TryingStatus},
			snapsToMake:    []snap.PlaceInfo{kernel1},
			expRebootPanic: "reboot due to try kernel file not existing",
			comment:        "fallback kernel upgrade path, due to try kernel file not existing",
		},
		// kernel upgrade path, but reboots to fallback due to invalid kernel_status
		{
			m:              &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()}},
			kernel:         kernel1,
			trykernel:      kernel2,
			typs:           []snap.Type{kernelT},
			blvars:         map[string]string{"kernel_status": boot.TryStatus},
			snapsToMake:    []snap.PlaceInfo{kernel1, kernel2},
			expRebootPanic: "reboot due to kernel_status wrong",
			comment:        "fallback kernel upgrade path, due to kernel_status wrong",
		},

		//
		// unhappy initramfs fail kernel paths
		//

		// fallback kernel not trusted in modeenv
		{
			m:           &boot.Modeenv{Mode: "run"},
			kernel:      kernel1,
			typs:        []snap.Type{kernelT},
			snapsToMake: []snap.PlaceInfo{kernel1},
			errPattern:  fmt.Sprintf("fallback kernel snap %q is not trusted in the modeenv", kernel1.Filename()),
			comment:     "fallback kernel not trusted in modeenv",
		},
		// fallback kernel file doesn't exist
		{
			m:          &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:     kernel1,
			typs:       []snap.Type{kernelT},
			errPattern: fmt.Sprintf("kernel snap %q does not exist on ubuntu-data", kernel1.Filename()),
			comment:    "fallback kernel file doesn't exist",
		},

		//
		// happy base upgrade paths
		//

		// successful base upgrade path
		{
			m: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.TryStatus,
			},
			expectedM: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.TryingStatus,
			},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1, base2},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base2},
			comment:     "successful base upgrade path",
		},
		// base upgrade path, but uses fallback due to try base file not existing
		{
			m: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.TryStatus,
			},
			expectedM: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.TryStatus,
			},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base1},
			comment:     "fallback base upgrade path, due to missing try base file",
		},
		// base upgrade path, but uses fallback due to base_status trying
		{
			m: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.TryingStatus,
			},
			expectedM: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.DefaultStatus,
			},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1, base2},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base1},
			comment:     "fallback base upgrade path, due to base_status trying",
		},
		// base upgrade path, but uses fallback due to base_status default
		{
			m: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.DefaultStatus,
			},
			expectedM: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    base2.Filename(),
				BaseStatus: boot.DefaultStatus,
			},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1, base2},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base1},
			comment:     "fallback base upgrade path, due to missing base_status",
		},

		//
		// unhappy base paths
		//

		// base snap unset
		{
			m:           &boot.Modeenv{Mode: "run"},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1},
			errPattern:  "fallback base snap unusable: cannot get snap revision: modeenv base boot variable is empty",
			comment:     "base snap unset in modeenv",
		},
		// base snap file doesn't exist
		{
			m:          &boot.Modeenv{Mode: "run", Base: base1.Filename()},
			typs:       []snap.Type{baseT},
			errPattern: fmt.Sprintf("base snap %q does not exist on ubuntu-data", base1.Filename()),
			comment:    "base snap unset in modeenv",
		},
		// unhappy, but silent path with fallback, due to invalid try base snap name
		{
			m: &boot.Modeenv{
				Mode:       "run",
				Base:       base1.Filename(),
				TryBase:    "bogusname",
				BaseStatus: boot.TryStatus,
			},
			typs:        []snap.Type{baseT},
			snapsToMake: []snap.PlaceInfo{base1},
			expected:    map[snap.Type]snap.PlaceInfo{baseT: base1},
			comment:     "corrupted base snap name",
		},

		//
		// combined cases
		//

		// default
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename()},
			},
			kernel:      kernel1,
			typs:        []snap.Type{baseT, kernelT},
			snapsToMake: []snap.PlaceInfo{base1, kernel1},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base1,
				kernelT: kernel1,
			},
			comment: "default combined kernel + base",
		},
		// combined, upgrade only the kernel
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			kernel:      kernel1,
			trykernel:   kernel2,
			typs:        []snap.Type{baseT, kernelT},
			blvars:      map[string]string{"kernel_status": boot.TryingStatus},
			snapsToMake: []snap.PlaceInfo{base1, kernel1, kernel2},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base1,
				kernelT: kernel2,
			},
			comment: "combined kernel + base, successful kernel upgrade",
		},
		// combined, upgrade only the base
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.TryStatus,
				CurrentKernels: []string{kernel1.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.TryingStatus,
				CurrentKernels: []string{kernel1.Filename()},
			},
			kernel:      kernel1,
			typs:        []snap.Type{baseT, kernelT},
			snapsToMake: []snap.PlaceInfo{base1, base2, kernel1},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base2,
				kernelT: kernel1,
			},
			comment: "combined kernel + base, successful base upgrade",
		},
		// bonus points: combined upgrade kernel and base
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.TryStatus,
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.TryingStatus,
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			kernel:      kernel1,
			trykernel:   kernel2,
			typs:        []snap.Type{baseT, kernelT},
			blvars:      map[string]string{"kernel_status": boot.TryingStatus},
			snapsToMake: []snap.PlaceInfo{base1, base2, kernel1, kernel2},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base2,
				kernelT: kernel2,
			},
			comment: "combined kernel + base, successful base + kernel upgrade",
		},
		// combined, fallback upgrade on kernel
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				CurrentKernels: []string{kernel1.Filename(), kernel2.Filename()},
			},
			kernel:      kernel1,
			trykernel:   kernel2,
			typs:        []snap.Type{baseT, kernelT},
			blvars:      map[string]string{"kernel_status": boot.DefaultStatus},
			snapsToMake: []snap.PlaceInfo{base1, kernel1, kernel2},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base1,
				kernelT: kernel1,
			},
			comment: "combined kernel + base, fallback kernel upgrade, due to missing boot var",
		},
		// combined, fallback upgrade on base
		{
			m: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.TryingStatus,
				CurrentKernels: []string{kernel1.Filename()},
			},
			expectedM: &boot.Modeenv{
				Mode:           "run",
				Base:           base1.Filename(),
				TryBase:        base2.Filename(),
				BaseStatus:     boot.DefaultStatus,
				CurrentKernels: []string{kernel1.Filename()},
			},
			kernel:      kernel1,
			typs:        []snap.Type{baseT, kernelT},
			snapsToMake: []snap.PlaceInfo{base1, base2, kernel1},
			expected: map[snap.Type]snap.PlaceInfo{
				baseT:   base1,
				kernelT: kernel1,
			},
			comment: "combined kernel + base, fallback base upgrade, due to base_status trying",
		},
	}

	// do both the normal uc20 bootloader and the env ref bootloader
	bloaderTable := []struct {
		bl interface {
			bootloader.Bootloader
			SetEnabledKernel(s snap.PlaceInfo) (restore func())
			SetEnabledTryKernel(s snap.PlaceInfo) (restore func())
		}
		name string
	}{
		{
			boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir())),
			"env ref extracted kernel",
		},
		{
			boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir())),
			"extracted run kernel image",
		},
	}

	for _, tbl := range bloaderTable {
		bl := tbl.bl
		for _, t := range tt {
			var cleanups []func()

			comment := Commentf("[%s] %s", tbl.name, t.comment)

			// we use a panic to simulate a reboot
			if t.expRebootPanic != "" {
				r := boot.MockInitramfsReboot(func() error {
					panic(t.expRebootPanic)
				})
				cleanups = append(cleanups, r)
			}

			bootloader.Force(bl)
			cleanups = append(cleanups, func() { bootloader.Force(nil) })

			// set the bl kernel / try kernel
			if t.kernel != nil {
				cleanups = append(cleanups, bl.SetEnabledKernel(t.kernel))
			}

			if t.trykernel != nil {
				cleanups = append(cleanups, bl.SetEnabledTryKernel(t.trykernel))
			}

			if t.blvars != nil {
				c.Assert(bl.SetBootVars(t.blvars), IsNil, comment)
				cleanBootVars := make(map[string]string, len(t.blvars))
				for k := range t.blvars {
					cleanBootVars[k] = ""
				}
				cleanups = append(cleanups, func() {
					c.Assert(bl.SetBootVars(cleanBootVars), IsNil, comment)
				})
			}

			if len(t.snapsToMake) != 0 {
				r := makeSnapFilesOnInitramfsUbuntuData(c, comment, t.snapsToMake...)
				cleanups = append(cleanups, r)
			}

			// write the modeenv to somewhere so we can read it and pass that to
			// InitramfsRunModeChooseSnapsToMount
			err := t.m.WriteTo(boot.InitramfsWritableDir)
			// remove it because we are writing many modeenvs in this single test
			cleanups = append(cleanups, func() {
				c.Assert(os.Remove(dirs.SnapModeenvFileUnder(boot.InitramfsWritableDir)), IsNil, Commentf(t.comment))
			})
			c.Assert(err, IsNil, comment)

			m, err := boot.ReadModeenv(boot.InitramfsWritableDir)
			c.Assert(err, IsNil, comment)

			if t.expRebootPanic != "" {
				f := func() { boot.InitramfsRunModeSelectSnapsToMount(t.typs, m) }
				c.Assert(f, PanicMatches, t.expRebootPanic, comment)
			} else {
				mountSnaps, err := boot.InitramfsRunModeSelectSnapsToMount(t.typs, m)
				if t.errPattern != "" {
					c.Assert(err, ErrorMatches, t.errPattern, comment)
				} else {
					c.Assert(err, IsNil, comment)
					c.Assert(mountSnaps, DeepEquals, t.expected, comment)
				}
			}

			// check that the modeenv changed as expected
			if t.expectedM != nil {
				newM, err := boot.ReadModeenv(boot.InitramfsWritableDir)
				c.Assert(err, IsNil, comment)
				c.Assert(newM.Base, Equals, t.expectedM.Base, comment)
				c.Assert(newM.BaseStatus, Equals, t.expectedM.BaseStatus, comment)
				c.Assert(newM.TryBase, Equals, t.expectedM.TryBase, comment)

				// shouldn't be changing in the initramfs, but be safe
				c.Assert(newM.CurrentKernels, DeepEquals, t.expectedM.CurrentKernels, comment)
			}

			// clean up
			for _, r := range cleanups {
				r()
			}
		}
	}
}
