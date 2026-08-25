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

package boot_test

import (
	"fmt"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type runBootChainsSuite struct {
	testutil.BaseTest

	rootdir string
}

var _ = Suite(&runBootChainsSuite{})

func (s *runBootChainsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *runBootChainsSuite) TestGetRunBootChains(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	// TODO: we should have multiple boot chains with only one valid. But bootloadertest does
	// not yet allow that.
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	bootFiles, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, IsNil)

	c.Check(bootFiles, DeepEquals, []bootloader.BootFile{
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:shim-hash-shim-recovery"),
			Snap:"",
			Role: "recovery",
		},
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:grub-hash-grub-recovery"),
			Snap: "",
			Role: "recovery",
		},
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:grub-hash-grub-run"),
			Snap: "",
			Role: "run-mode",
		},
		{
			Path: "kernel.efi",
			Snap: filepath.Join(s.rootdir, "/var/lib/snapd/snaps/some-kernel_x1_unset.snap"),
			Role: "run-mode",
		},
	})
}

func (s *runBootChainsSuite) TestGetRunBootChainsTryKernel(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})
	runBl.SetEnabledTryKernel(&snap.Info{SuggestedName: "some-try-kernel", InstanceKey: "x2", SnapType: snap.TypeKernel})

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	bootFiles, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, IsNil)

	c.Check(bootFiles, DeepEquals, []bootloader.BootFile{
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:shim-hash-shim-recovery"),
			Snap:"",
			Role: "recovery",
		},
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:grub-hash-grub-recovery"),
			Snap: "",
			Role: "recovery",
		},
		{
			Path: filepath.Join(s.rootdir, "/var/lib/snapd/boot-assets/run/ubuntu:grub-hash-grub-run"),
			Snap: "",
			Role: "run-mode",
		},
		{
			Path: "kernel.efi",
			Snap: filepath.Join(s.rootdir, "/var/lib/snapd/snaps/some-try-kernel_x2_unset.snap"),
			Role: "run-mode",
		},
	})
}

func (s *runBootChainsSuite) TestGetRunBootChainsNoKernel(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetRunKernelImageFunctionError("Kernel", fmt.Errorf("boom"))

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, "boom")
}

func (s *runBootChainsSuite) TestGetRunBootChainsFailedTryKernel(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})
	runBl.SetRunKernelImageFunctionError("TryKernel", fmt.Errorf("boom"))

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, `boom`)
}

func (s *runBootChainsSuite) TestGetRunBootChainsNoRecoveryBl(c *C) {
	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return nil, fmt.Errorf("boom")
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, `cannot find recovery bootloader: boom`)
}

func (s *runBootChainsSuite) TestGetRunBootChainsBadRecoveryBl(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "")

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, `internal error: recovery bootloader does not support trusted assets`)
}

func (s *runBootChainsSuite) TestGetRunBootChainsNoRunBl(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return nil, fmt.Errorf("boom")
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, `cannot find run bootloader: boom`)
}

func (s *runBootChainsSuite) TestGetRunBootChainsBadRunBl(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile(kernelPath, "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "")

	defer boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	modeenv := boot.Modeenv{
		CurrentTrustedBootAssets: map[string][]string{
			"ubuntu:grub": {
				"hash-grub-run",
			},
		},
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"ubuntu:shim": {
				"hash-shim-recovery",
			},
			"ubuntu:grub": {
				"hash-grub-recovery",
			},
		},
	}

	_, err := boot.GetRunBootChain(&modeenv)
	c.Assert(err, ErrorMatches, `internal error: run bootloader does not support kernel extraction`)
}
