// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

func init() {
	const (
		short = "Generate initramfs mount tuples"
		long  = "Generate mount tuples for the initramfs until nothing more can be done"
	)

	if _, err := parser.AddCommand("initramfs-mounts", short, long, &cmdInitramfsMounts{}); err != nil {
		panic(err)
	}
}

type cmdInitramfsMounts struct{}

func (c *cmdInitramfsMounts) Execute(args []string) error {
	return generateInitramfsMounts()
}

var (
	// the kernel commandline - can be overridden in tests
	procCmdline = "/proc/cmdline"

	// Stdout - can be overridden in tests
	stdout io.Writer = os.Stdout
)

var (
	runMnt = "/run/mnt"

	osutilIsMounted = osutil.IsMounted
)

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall() error {
	// 1. always ensure seed partition is mounted
	isMounted, err := osutilIsMounted(filepath.Join(runMnt, "ubuntu-seed"))
	if err != nil {
		return err
	}
	if !isMounted {
		fmt.Fprintf(stdout, "/dev/disk/by-label/ubuntu-seed %s\n", filepath.Join(runMnt, "ubuntu-seed"))
		return nil
	}
	// XXX: how do we select a different recover system from the cmdline?

	// 2. (auto) select recovery system for now
	isMounted, err = osutilIsMounted(filepath.Join(runMnt, "base"))
	if err != nil {
		return err
	}
	if !isMounted {
		l, err := filepath.Glob(filepath.Join(runMnt, "/ubuntu-seed/systems/*"))
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return fmt.Errorf("cannot find a recovery system")
		}
		if len(l) > 1 {
			return fmt.Errorf("cannot use multiple recovery systems yet")
		}
		// load the seed and generate mounts for kernel/base
		label := filepath.Base(l[0])
		seedDir := filepath.Dir(filepath.Dir(l[0]))
		deviceSeed, err := seed.Open(seedDir, label)
		if err != nil {
			return err
		}
		// load assertions into a temporary database
		if err := deviceSeed.LoadAssertions(nil, nil); err != nil {
			return err
		}
		perf := timings.New(nil)
		// XXX: LoadMeta will verify all the snaps in the
		// seed, that is probably too much. We can expose more
		// dedicated helpers for this later.
		if err := deviceSeed.LoadMeta(perf); err != nil {
			return err
		}
		// XXX: do we need more cross checks here?
		for _, essentialSnap := range deviceSeed.EssentialSnaps() {
			snapf, err := snap.Open(essentialSnap.Path)
			if err != nil {
				return err
			}
			info, err := snap.ReadInfoFromSnapFile(snapf, essentialSnap.SideInfo)
			if err != nil {
				return err
			}
			switch info.GetType() {
			case snap.TypeBase:
				fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(runMnt, "base"))
			case snap.TypeKernel:
				fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(runMnt, "kernel"))
			}
		}

		return nil
	}

	// 3. mount "ubuntu-data" on a tmpfs
	isMounted, err = osutilIsMounted(filepath.Join(runMnt, "ubuntu-data"))
	if err != nil {
		return err
	}
	if !isMounted {
		// XXX: is there a better way?
		fmt.Fprintf(stdout, "--type=tmpfs tmpfs /run/mnt/ubuntu-data\n")
		return nil
	}

	// 4. done, no output, no error indicates to initramfs we are done
	//    with mounting stuff
	return nil
}

func generateMountsModeRecover() error {
	return fmt.Errorf("recover mode mount generation not implemented yet")
}

func generateMountsModeRun() error {
	return fmt.Errorf("run mode mount generation not implemented yet")
}

func isInstallMode(content []byte) bool {
	// XXX: deal with whitespace
	if bytes.Contains(content, []byte("snapd_recovery_mode=install")) {
		return true
	}
	// no snapd_recovery_mode var set -> assume install mode
	if bytes.Contains(content, []byte("snapd_recovery_mode= ")) || bytes.HasSuffix(content, []byte("snapd_recovery_mode=")) {
		return true
	}
	return false
}

func isRecoverMode(content []byte) bool {
	// XXX: deal with whitespace
	return bytes.Contains(content, []byte("snapd_recovery_mode=recover"))
}

func isRunMode(content []byte) bool {
	// XXX: deal with whitespace XXX2: The "snap-bootstrap
	// initramfs-mounts" helper will run in both if we run from a
	// recovery grub or from a normal grub. However in the normal
	// case we probably have no "snapd_recovery_mode" set. So we
	// may tweak this later to assume run-mode if nothing is set.
	return bytes.Contains(content, []byte("snapd_recovery_mode=run"))
}

func generateInitramfsMounts() error {
	content, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return err
	}
	switch {
	case isRecoverMode(content):
		return generateMountsModeRecover()
	case isInstallMode(content):
		return generateMountsModeInstall()
	case isRunMode(content):
		return generateMountsModeRun()
	default:
		return fmt.Errorf("cannot detect if in run,install,recover mode")
	}
}
