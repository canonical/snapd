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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
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

// XXX: make this more flexible if there are multiple seeds on disk, i.e.
// read kernel commandline in this case (bootenv is off limits because
// it's not measured).
func findRecoverySystem(seedDir string) (systemLabel string, err error) {
	l, err := filepath.Glob(filepath.Join(seedDir, "systems/*"))
	if err != nil {
		return "", err
	}
	if len(l) == 0 {
		return "", fmt.Errorf("cannot find a recovery system")
	}
	if len(l) > 1 {
		return "", fmt.Errorf("cannot use multiple recovery systems yet")
	}
	systemLabel = filepath.Base(l[0])
	return systemLabel, nil
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall() error {
	seedDir := filepath.Join(runMnt, "ubuntu-seed")

	// 1. always ensure seed partition is mounted
	isMounted, err := osutilIsMounted(seedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		fmt.Fprintf(stdout, "/dev/disk/by-label/ubuntu-seed %s\n", seedDir)
		return nil
	}
	// XXX: how do we select a different recover system from the cmdline?

	// 2. (auto) select recovery system for now
	isMounted, err = osutilIsMounted(filepath.Join(runMnt, "base"))
	if err != nil {
		return err
	}
	if !isMounted {
		// load the recovery system  and generate mounts for kernel/base
		systemLabel, err := findRecoverySystem(seedDir)
		if err != nil {
			return err
		}
		systemSeed, err := seed.Open(seedDir, systemLabel)
		if err != nil {
			return err
		}
		// load assertions into a temporary database
		if err := systemSeed.LoadAssertions(nil, nil); err != nil {
			return err
		}
		perf := timings.New(nil)
		// XXX: LoadMeta will verify all the snaps in the
		// seed, that is probably too much. We can expose more
		// dedicated helpers for this later.
		if err := systemSeed.LoadMeta(perf); err != nil {
			return err
		}
		// XXX: do we need more cross checks here?
		for _, essentialSnap := range systemSeed.EssentialSnaps() {
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

	// 4. final step: write $(ubuntu_data)/var/lib/snapd/modeenv - this
	//    is the tmpfs we just created above
	systemLabel, err := findRecoverySystem(seedDir)
	if err != nil {
		return err
	}
	modeEnv := &boot.Modeenv{
		Mode:           "install",
		RecoverySystem: systemLabel,
	}
	if err := modeEnv.Write(filepath.Join(runMnt, "ubuntu-data")); err != nil {
		return err
	}

	// 5. done, no output, no error indicates to initramfs we are done
	//    with mounting stuff
	return nil
}

func generateMountsModeRecover() error {
	return fmt.Errorf("recover mode mount generation not implemented yet")
}

func generateMountsModeRun() error {
	return fmt.Errorf("run mode mount generation not implemented yet")
}

var validModes = []string{"install", "recover", "run"}

func whichMode(content []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(content))
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "snapd_recovery_mode=") {
			mode := strings.SplitN(scanner.Text(), "=", 2)[1]
			if mode == "" {
				mode = "install"
			}
			if !strutil.ListContains(validModes, mode) {
				return "", fmt.Errorf("cannot use unknown mode %q", mode)
			}
			return mode, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("cannot detect if in run,install,recover mode")
}

func generateInitramfsMounts() error {
	content, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return err
	}
	mode, err := whichMode(content)
	if err != nil {
		return err
	}
	switch mode {
	case "recover":
		return generateMountsModeRecover()
	case "install":
		return generateMountsModeInstall()
	case "run":
		return generateMountsModeRun()
	}
	// this should never be reached
	return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
}
