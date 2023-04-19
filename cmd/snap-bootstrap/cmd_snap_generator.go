// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	const (
		short = "Snap system generator"
		long  = "Snap system generator"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("snap-generator", short, long, &cmdSnapGenerator{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdSnapGenerator struct {
	Positional struct {
		NormalDir string `required:"yes"`
		EarlyDir  string `required:"yes"`
		LateDir   string `required:"yes"`
	} `positional-args:"yes"`
}

func (c *cmdSnapGenerator) Execute([]string) error {
	return snapGenerator(c.Positional.NormalDir, c.Positional.EarlyDir, c.Positional.LateDir)
}

var (
	mountUnitTemplate = `
[Unit]
DefaultDependencies=no
Before=%s

[Mount]
What=%s
Where=%s
Type=squashfs
Options=ro,private
`
)

func symlinkSysroot(target string) error {
	if err := os.MkdirAll("/run/mnt", 0755); err != nil {
		return err
	}

	if err := os.Symlink(target, "/run/mnt/sysroot"); err != nil {
		if os.IsExist(err) {
			foundTarget, errReadlink := os.Readlink("/run/mnt/sysroot")
			if errReadlink != nil {
				return errReadlink
			}
			if target != foundTarget {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func generateInitrdMount(runDir string, what string, where string) error {
	unitName := fmt.Sprintf("%s.mount", systemd.EscapeUnitNamePath(where))
	unitPath := filepath.Join(runDir, unitName)

	initrdTarget := "initrd-root-fs.target"
	initrdWants := filepath.Join(runDir, fmt.Sprintf("%s.wants", initrdTarget))

	if err := os.MkdirAll(initrdWants, 0755); err != nil {
		return err
	}

	unitFile, err := os.OpenFile(unitPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer unitFile.Close()
	if _, err := fmt.Fprintf(unitFile, mountUnitTemplate, initrdTarget, what, where); err != nil {
		return err
	}

	if err := os.Symlink(filepath.Join("..", unitName), filepath.Join(initrdWants, unitName)); err != nil {
		return err
	}

	return err
}

func snapGeneratorRun(normalDir string, earlyDir string, lateDir string) error {
	isBootMounted, err := osutil.IsMounted(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot"))
	if err != nil {
		return err
	}
	if !isBootMounted {
		return nil
	}

	isDataMounted, err := osutil.IsMounted(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data"))
	if err != nil {
		return err
	}
	if !isDataMounted {
		return nil
	}

	model, err := getUnverifiedBootModel()
	if err != nil {
		return err
	}

	isClassic := model.Classic()

	rootfsDir := boot.InitramfsWritableDir(model, true)

	modeEnv, err := boot.ReadModeenv(rootfsDir)
	if err != nil {
		return err
	}

	typs := []snap.Type{snap.TypeGadget, snap.TypeKernel}
	if !isClassic {
		typs = append([]snap.Type{snap.TypeBase}, typs...)
	}

	mounts, err := boot.InitramfsRunModeSelectSnapsToMount(typs, modeEnv, rootfsDir)
	if err != nil {
		return err
	}

	for _, typ := range typs {
		if sn, ok := mounts[typ]; ok {
			dir := snapTypeToMountDir[typ]
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), sn.Filename())
			if err := generateInitrdMount(normalDir, snapPath, filepath.Join(boot.InitramfsRunMntDir, dir)); err != nil {
				return err
			}
		}
	}

	if modeEnv.RecoverySystem != "" && !isClassic {
		isSeedMounted, err := osutil.IsMounted(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed"))
		if err != nil {
			return err
		}
		// If seed is not mounted, we will be re-loaded again
		if isSeedMounted {
			_, essSnaps, err := readEssential(modeEnv.RecoverySystem, []snap.Type{snap.TypeSnapd})
			if err != nil {
				return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
			}
			if err := generateInitrdMount(normalDir, essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd")); err != nil {
				return fmt.Errorf("cannot mount snapd snap: %v", err)
			}
		}
	}

	var target string
	if isClassic {
		target = "data"
	} else {
		target = "base"
	}
	if err := symlinkSysroot(target); err != nil {
		return err
	}

	return nil
}

func snapGeneratorInstall(recoverySystem string, normalDir string, earlyDir string, lateDir string) error {
	isSeedMounted, err := osutil.IsMounted(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed"))
	if err != nil {
		return err
	}
	if !isSeedMounted {
		return nil
	}

	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}
	_, essSnaps, err := readEssential(recoverySystem, typs)
	if err != nil {
		return fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", typs, err)
	}

	systemSnaps := make(map[snap.Type]snap.PlaceInfo)

	for _, essentialSnap := range essSnaps {
		systemSnaps[essentialSnap.EssentialType] = essentialSnap.PlaceInfo()
		dir := snapTypeToMountDir[essentialSnap.EssentialType]
		// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
		if err := generateInitrdMount(normalDir, essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, dir)); err != nil {
			return err
		}
	}

	if err := symlinkSysroot("base"); err != nil {
		return err
	}

	return nil
}

func snapGenerator(normalDir string, earlyDir string, lateDir string) error {
	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	switch mode {
	case "run":
		return snapGeneratorRun(normalDir, earlyDir, lateDir)
	case "recover":
	case "install":
		return snapGeneratorInstall(recoverySystem, normalDir, earlyDir, lateDir)
	case "factory-reset":
	case "cloudimg-rootfs":
	default:
		return fmt.Errorf("internal error: mode in snap-generator not handled")
	}
	return nil
}
