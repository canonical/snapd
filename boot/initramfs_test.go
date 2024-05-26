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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/osutil/kcmdline"
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
	mylog.
		// with no bootloader available we can't mark successful
		Check(boot.EnsureNextBootToRunMode("label"))
	c.Assert(err, ErrorMatches, "cannot determine bootloader")

	// forcing a bootloader works
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	mylog.Check(boot.EnsureNextBootToRunMode("label"))


	// the bloader vars have been updated
	m := mylog.Check2(bloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system"))

	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "label",
	})
}

func (s *initramfsSuite) TestEnsureNextBootToRunModeRealBootloader(c *C) {
	mylog.
		// create a real grub.cfg on ubuntu-seed
		Check(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu", "grub.cfg"), nil, 0644))

	mylog.Check(boot.EnsureNextBootToRunMode("somelabel"))


	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	bloader := mylog.Check2(bootloader.Find(boot.InitramfsUbuntuSeedDir, opts))

	c.Assert(bloader.Name(), Equals, "grub")

	// the bloader vars have been updated
	m := mylog.Check2(bloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system"))

	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "somelabel",
	})
}

func makeSnapFilesOnInitramfsUbuntuData(c *C, rootfsDir string, comment CommentInterface, snaps ...snap.PlaceInfo) (restore func()) {
	// also make sure the snaps also exist on ubuntu-data
	snapDir := dirs.SnapBlobDirUnder(rootfsDir)
	mylog.Check(os.MkdirAll(snapDir, 0755))
	c.Assert(err, IsNil, comment)
	paths := make([]string, 0, len(snaps))
	for _, sn := range snaps {
		snPath := filepath.Join(snapDir, sn.Filename())
		paths = append(paths, snPath)
		mylog.Check(os.WriteFile(snPath, nil, 0644))
		c.Assert(err, IsNil, comment)
	}
	return func() {
		for _, path := range paths {
			mylog.Check(os.Remove(path))
			c.Assert(err, IsNil, comment)
		}
	}
}

func (s *initramfsSuite) TestInitramfsRunModeSelectSnapsToMount(c *C) {
	// make some snap infos we will use in the tests
	kernel1 := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap"))


	kernel2 := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap"))


	base1 := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("core20_1.snap"))


	base2 := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("core20_2.snap"))


	gadget := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc_1.snap"))


	baseT := snap.TypeBase
	kernelT := snap.TypeKernel
	gadgetT := snap.TypeGadget

	tt := []struct {
		m                      *boot.Modeenv
		expectedM              *boot.Modeenv
		typs                   []snap.Type
		kernel                 snap.PlaceInfo
		trykernel              snap.PlaceInfo
		blvars                 map[string]string
		snapsToMake            []snap.PlaceInfo
		expected               map[snap.Type]snap.PlaceInfo
		errPattern             string
		expRebootPanic         string
		rootfsDir              string
		comment                string
		runBootEnvMethodToFail string
		runBootEnvError        error
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "default base path",
		},
		// gadget base path
		{
			m:           &boot.Modeenv{Mode: "run", Gadget: gadget.Filename()},
			typs:        []snap.Type{gadgetT},
			snapsToMake: []snap.PlaceInfo{gadget},
			expected:    map[snap.Type]snap.PlaceInfo{gadgetT: gadget},
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "default gadget path",
		},
		// gadget base path, but not in modeenv, so it is not selected
		{
			m:           &boot.Modeenv{Mode: "run"},
			typs:        []snap.Type{gadgetT},
			snapsToMake: []snap.PlaceInfo{gadget},
			expected:    map[snap.Type]snap.PlaceInfo{},
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "default gadget path",
		},
		// default kernel path
		{
			m:           &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:      kernel1,
			typs:        []snap.Type{kernelT},
			snapsToMake: []snap.PlaceInfo{kernel1},
			expected:    map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "default kernel path",
		},
		// gadget base path for classic with modes
		{
			m:           &boot.Modeenv{Mode: "run", Gadget: gadget.Filename()},
			typs:        []snap.Type{gadgetT},
			snapsToMake: []snap.PlaceInfo{gadget},
			expected:    map[snap.Type]snap.PlaceInfo{gadgetT: gadget},
			rootfsDir:   boot.InitramfsDataDir,
			comment:     "default gadget path for classic with modes",
		},
		// default kernel path for classic with modes
		{
			m:           &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:      kernel1,
			typs:        []snap.Type{kernelT},
			snapsToMake: []snap.PlaceInfo{kernel1},
			expected:    map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			rootfsDir:   boot.InitramfsDataDir,
			comment:     "default kernel path for classic with modes",
		},
		// dangling link for try kernel should be ignored if not trying status
		{
			m:                      &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), "pc-kernel_badrev.snap"}},
			kernel:                 kernel1,
			typs:                   []snap.Type{kernelT},
			blvars:                 map[string]string{"kernel_status": boot.DefaultStatus},
			snapsToMake:            []snap.PlaceInfo{kernel1},
			expected:               map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			rootfsDir:              filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:                "bad try kernel but we don't reboot",
			runBootEnvMethodToFail: "TryKernel",
			runBootEnvError:        fmt.Errorf("cannot read dangling symlink"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:      filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:      filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:      filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:        "fallback kernel upgrade path, due to kernel_status wrong",
		},
		// bad try status and no try kernel found
		{
			m:                      &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename(), "pc-kernel_badrev.snap"}},
			kernel:                 kernel1,
			typs:                   []snap.Type{kernelT},
			blvars:                 map[string]string{"kernel_status": boot.TryStatus},
			snapsToMake:            []snap.PlaceInfo{kernel1},
			expected:               map[snap.Type]snap.PlaceInfo{kernelT: kernel1},
			rootfsDir:              filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:                "bad try status, we reboot",
			runBootEnvMethodToFail: "TryKernel",
			runBootEnvError:        fmt.Errorf("cannot read dangling symlink"),
			expRebootPanic:         "reboot due to bad try status",
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "fallback kernel not trusted in modeenv",
		},
		// fallback kernel file doesn't exist
		{
			m:          &boot.Modeenv{Mode: "run", CurrentKernels: []string{kernel1.Filename()}},
			kernel:     kernel1,
			typs:       []snap.Type{kernelT},
			errPattern: fmt.Sprintf("kernel snap %q does not exist on ubuntu-data", kernel1.Filename()),
			rootfsDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			errPattern:  "no currently usable base snaps: cannot get snap revision: modeenv base boot variable is empty",
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:     "base snap unset in modeenv",
		},
		// base snap file doesn't exist
		{
			m:          &boot.Modeenv{Mode: "run", Base: base1.Filename()},
			typs:       []snap.Type{baseT},
			errPattern: fmt.Sprintf("base snap %q does not exist on ubuntu-data", base1.Filename()),
			rootfsDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "default combined kernel + base",
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "combined kernel + base, successful kernel upgrade",
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "combined kernel + base, successful base upgrade",
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "combined kernel + base, successful base + kernel upgrade",
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "combined kernel + base, fallback kernel upgrade, due to missing boot var",
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
			rootfsDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"),
			comment:   "combined kernel + base, fallback base upgrade, due to base_status trying",
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
			if t.runBootEnvMethodToFail != "" {
				if rbe, ok := tbl.bl.(*boottest.RunBootenv20); ok {
					cleanups = append(cleanups, rbe.MockExtractedRunKernelImageMixin.SetRunKernelImageFunctionError(
						t.runBootEnvMethodToFail, t.runBootEnvError))
				}
			}

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
				r := makeSnapFilesOnInitramfsUbuntuData(c, t.rootfsDir, comment, t.snapsToMake...)
				cleanups = append(cleanups, r)
			}
			mylog.Check(

				// write the modeenv to somewhere so we can read it and pass that to
				// InitramfsRunModeChooseSnapsToMount
				t.m.WriteTo(t.rootfsDir))
			// remove it because we are writing many modeenvs in this single test
			cleanups = append(cleanups, func() {
				c.Assert(os.Remove(dirs.SnapModeenvFileUnder(t.rootfsDir)), IsNil, Commentf(t.comment))
			})
			c.Assert(err, IsNil, comment)

			m := mylog.Check2(boot.ReadModeenv(t.rootfsDir))
			c.Assert(err, IsNil, comment)

			if t.expRebootPanic != "" {
				f := func() { boot.InitramfsRunModeSelectSnapsToMount(t.typs, m, t.rootfsDir) }
				c.Assert(f, PanicMatches, t.expRebootPanic, comment)
			} else {
				mountSnaps := mylog.Check2(boot.InitramfsRunModeSelectSnapsToMount(t.typs, m, t.rootfsDir))
				if t.errPattern != "" {
					c.Assert(err, ErrorMatches, t.errPattern, comment)
				} else {
					c.Assert(err, IsNil, comment)
					c.Assert(mountSnaps, DeepEquals, t.expected, comment)
				}
			}

			// check that the modeenv changed as expected
			if t.expectedM != nil {
				newM := mylog.Check2(boot.ReadModeenv(t.rootfsDir))
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

func (s *initramfsSuite) TestInitramfsRunModeUpdateBootloaderVars(c *C) {
	bloader := bootloadertest.Mock("noscripts", c.MkDir()).WithNotScriptable()
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	tt := []struct {
		cmdline       string
		initialStatus string
		finalStatus   string
	}{
		{
			cmdline:       "kernel_status=trying",
			initialStatus: "try",
			finalStatus:   "trying",
		},
		{
			cmdline:       "kernel_status=trying",
			initialStatus: "badstate",
			finalStatus:   "",
		},
		{
			cmdline:       "kernel_status=trying",
			initialStatus: "",
			finalStatus:   "",
		},
		{
			cmdline:       "",
			initialStatus: "try",
			finalStatus:   "",
		},
		{
			cmdline:       "",
			initialStatus: "trying",
			finalStatus:   "",
		},
		{
			cmdline:       "quiet splash",
			initialStatus: "try",
			finalStatus:   "",
		},
	}

	for _, t := range tt {
		bloader.SetBootVars(map[string]string{"kernel_status": t.initialStatus})

		cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
		mylog.Check(os.WriteFile(cmdlineFile, []byte(t.cmdline), 0644))

		r := kcmdline.MockProcCmdline(cmdlineFile)
		defer r()
		mylog.Check(boot.InitramfsRunModeUpdateBootloaderVars())

		vars := mylog.Check2(bloader.GetBootVars("kernel_status"))

		c.Assert(vars, DeepEquals, map[string]string{"kernel_status": t.finalStatus})
	}
}

func (s *initramfsSuite) TestInitramfsRunModeUpdateBootloaderVarsNotNotScriptable(c *C) {
	// Make sure the method does not change status if the
	// bootloader does not implement NotScriptableBootloader

	bloader := bootloadertest.Mock("noscripts", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.SetBootVars(map[string]string{"kernel_status": "try"})

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	mylog.Check(os.WriteFile(cmdlineFile, []byte("kernel_status=trying"), 0644))

	r := kcmdline.MockProcCmdline(cmdlineFile)
	defer r()
	mylog.Check(boot.InitramfsRunModeUpdateBootloaderVars())

	vars := mylog.Check2(bloader.GetBootVars("kernel_status"))

	c.Assert(vars, DeepEquals, map[string]string{"kernel_status": "try"})
}

func (s *initramfsSuite) TestInitramfsRunModeUpdateBootloaderVarsErrOnGetBootVars(c *C) {
	bloader := bootloadertest.Mock("noscripts", c.MkDir()).WithNotScriptable()
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	errMsg := "cannot get boot environment"
	bloader.GetErr = fmt.Errorf(errMsg)

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	mylog.Check(os.WriteFile(cmdlineFile, []byte("kernel_status=trying"), 0644))

	r := kcmdline.MockProcCmdline(cmdlineFile)
	defer r()
	mylog.Check(boot.InitramfsRunModeUpdateBootloaderVars())
	c.Assert(err, ErrorMatches, errMsg)
}

func (s *initramfsSuite) TestInitramfsRunModeUpdateBootloaderVarsErrNoCmdline(c *C) {
	bloader := bootloadertest.Mock("noscripts", c.MkDir()).WithNotScriptable()
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.SetBootVars(map[string]string{"kernel_status": "try"})
	mylog.Check(boot.InitramfsRunModeUpdateBootloaderVars())
	c.Assert(err, ErrorMatches, ".*cmdline: no such file or directory")
}

func (s *initramfsSuite) TestInitramfsRunModeUpdateBootloaderVarsNoBootloaderHappy(c *C) {
	mylog.Check(boot.InitramfsRunModeUpdateBootloaderVars())

}

var classicModel = &gadgettest.ModelCharacteristics{
	IsClassic: true,
	HasModes:  true,
}

var coreModel = &gadgettest.ModelCharacteristics{
	IsClassic: false,
	HasModes:  true,
}

func (s *initramfsSuite) TestInstallHostWritableDir(c *C) {
	c.Check(boot.InstallHostWritableDir(classicModel), Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data"))
	c.Check(boot.InstallHostWritableDir(coreModel), Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"))
}

func (s *initramfsSuite) TestInitramfsHostWritableDir(c *C) {
	c.Check(boot.InitramfsHostWritableDir(classicModel), Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data"))
	c.Check(boot.InitramfsHostWritableDir(coreModel), Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"))
}

func (s *initramfsSuite) TestInitramfsWritableDir(c *C) {
	for _, tc := range []struct {
		model       gadget.Model
		runMode     bool
		expectedDir string
	}{
		{classicModel, true, "/run/mnt/data"},
		{classicModel, false, "/run/mnt/data/system-data"},
		{coreModel, true, "/run/mnt/data/system-data"},
		{coreModel, false, "/run/mnt/data/system-data"},
	} {
		c.Check(boot.InitramfsWritableDir(tc.model, tc.runMode), Equals, filepath.Join(dirs.GlobalRootDir, tc.expectedDir))
	}
}
