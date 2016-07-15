// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package weld

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
)

type Options struct {
	Snaps            []string
	Rootdir          string
	Channel          string
	ModelAssertionFn string
	GadgetUnpackDir  string
}

func Weld(opts *Options) error {
	if err := downloadUnpackGadget(opts); err != nil {
		return err
	}

	// FIXME: seed.yaml support (once that is better defined)
	//        for e.g. channel per snap
	return bootstrapToRootdir(opts)
}

func decodeModelAssertion(fn string) (*asserts.Model, error) {
	rawAssert, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, err
	}

	ass, err := asserts.Decode(rawAssert)
	if err != nil {
		return nil, err
	}
	return ass.(*asserts.Model), nil
}

func downloadUnpackGadget(opts *Options) error {
	model, err := decodeModelAssertion(opts.ModelAssertionFn)
	if err != nil {
		return err
	}

	dlOpts := &downloadOptions{
		TargetDir:    opts.GadgetUnpackDir,
		Channel:      opts.Channel,
		StoreID:      model.Store(),
		Architecture: model.Architecture(),
	}
	snapFn, err := acquireSnap(model.Gadget(), dlOpts)
	if err != nil {
		return err
	}
	// FIXME: jumping through layers here, we need to make
	//        unpack part of the container interface (again)
	snap := squashfs.New(snapFn)
	return snap.Unpack("*", opts.GadgetUnpackDir)
}

func acquireSnap(snapName string, dlOpts *downloadOptions) (string, error) {
	if osutil.FileExists(snapName) {
		return copyLocalSnapFile(snapName, dlOpts.TargetDir)
	}

	return downloadSnapWithSideInfo(snapName, dlOpts)
}

func bootstrapToRootdir(opts *Options) error {
	if opts.Rootdir != "" {
		dirs.SetRootDir(opts.Rootdir)
		defer dirs.SetRootDir("/")
	}

	// sanity check target
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot bootstrap over existing system")
	}

	model, err := decodeModelAssertion(opts.ModelAssertionFn)
	if err != nil {
		return err
	}

	// put snaps in place
	if err := os.MkdirAll(dirs.SnapBlobDir, 0755); err != nil {
		return err
	}

	snapSeedDir := filepath.Join(dirs.SnapSeedDir, "snaps")
	dlOpts := &downloadOptions{
		TargetDir:    snapSeedDir,
		Channel:      opts.Channel,
		StoreID:      model.Store(),
		Architecture: model.Architecture(),
	}

	// FIXME: support sideloading snaps by copying the boostrap.snaps
	//        first and keeping track of the already downloaded names
	snaps := []string{}
	snaps = append(snaps, opts.Snaps...)
	snaps = append(snaps, model.Gadget())
	snaps = append(snaps, model.Core())
	snaps = append(snaps, model.Kernel())
	snaps = append(snaps, model.RequiredSnaps()...)

	for _, d := range []string{dirs.SnapBlobDir, snapSeedDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	for _, snapName := range snaps {
		fmt.Printf("Fetching %s\n", snapName)
		fn, err := acquireSnap(snapName, dlOpts)
		if err != nil {
			return err
		}
		// kernel/os are required for booting
		if snapName == model.Kernel() || snapName == model.Core() {
			if err := osutil.CopyFile(fn, filepath.Join(dirs.SnapBlobDir, filepath.Base(fn)), 0); err != nil {
				return err
			}
		}
	}

	// now do the bootloader stuff
	if err := partition.InstallBootConfig(opts.GadgetUnpackDir); err != nil {
		return err
	}

	if err := setBootvars(); err != nil {
		return err
	}

	return nil
}

func setBootvars() error {
	// set the bootvars for kernel/os snaps so that the system
	// actually boots and can do the `firstboot` import of the snaps.
	//
	// there is also no mounted os/kernel snap, all we have are the
	// blobs

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("can not set kernel/os bootvars: %s", err)
	}

	snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap"))
	if len(snaps) == 0 {
		return fmt.Errorf("internal error: cannot find os/kernel snap")
	}
	for _, fullname := range snaps {
		bootvar := ""
		bootvar2 := ""

		// detect type
		snapFile, err := snap.Open(fullname)
		if err != nil {
			return fmt.Errorf("can not read %v", fullname)
		}
		// read .sideinfo
		var si snap.SideInfo
		siFn := fullname + ".sideinfo"
		if osutil.FileExists(siFn) {
			j, err := ioutil.ReadFile(siFn)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(j, &si); err != nil {
				return fmt.Errorf("cannot read metadata: %s %s\n", siFn, err)
			}
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, &si)
		if err != nil {
			return fmt.Errorf("can not get info for %v", fullname)
		}
		// local install
		if info.Revision.Unset() {
			info.Revision = snap.R(-1)
		}

		switch info.Type {
		case snap.TypeOS:
			bootvar = "snappy_os"
			bootvar2 = "snappy_good_os"
		case snap.TypeKernel:
			bootvar = "snappy_kernel"
			bootvar2 = "snappy_good_kernel"
			if err := extractKernelAssets(fullname, info); err != nil {
				return err
			}
		}

		name := filepath.Base(fullname)
		for _, b := range []string{bootvar, bootvar2} {
			if b != "" {
				if err := bootloader.SetBootVar(b, name); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func runCommand(cmdStr ...string) error {
	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot run %v: %s (%s)", cmdStr, err, output)
	}
	return nil
}

func extractKernelAssets(snapPath string, info *snap.Info) error {
	// FIXME: hrm, hrm, we need to be root for this - alternatively
	//        we could make boot.ExtractKernelAssets() work on
	//        a plain .snap file again (i.e. revert bee59a2)
	if err := os.MkdirAll(info.MountDir(), 0755); err != nil {
		return err
	}
	defer os.Remove(filepath.Dir(info.MountDir()))
	defer os.Remove(info.MountDir())

	if err := runCommand("mount", snapPath, info.MountDir()); err != nil {
		return err
	}
	defer runCommand("umount", info.MountDir())

	pb := progress.NewTextProgress()
	if err := boot.ExtractKernelAssets(info, pb); err != nil {
		return err
	}
	return nil
}

func copyLocalSnapFile(snapName, targetDir string) (string, error) {
	snapFile, err := snap.Open(snapName)
	if err != nil {
		return "", err
	}
	info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
	if err != nil {
		return "", err
	}
	// local snap gets sideloaded revision
	if info.Revision.Unset() {
		info.Revision = snap.R(-1)
	}
	dst := filepath.Join(targetDir, filepath.Dir(info.MountFile()))

	return dst, osutil.CopyFile(snapName, dst, 0)
}

type downloadOptions struct {
	Series       string
	TargetDir    string
	StoreID      string
	Channel      string
	Architecture string
}

// FIXME: move to snapstate next to InstallPathWithSideInfo()
func downloadSnapWithSideInfo(name string, opts *downloadOptions) (string, error) {
	if opts == nil {
		opts = &downloadOptions{}
	}

	if opts.Series != "" {
		oldSeries := release.Series
		defer func() { release.Series = oldSeries }()

		release.Series = opts.Series
	}
	if opts.Architecture != "" {
		oldArchitecture := arch.UbuntuArchitecture()
		defer func() { arch.SetArchitecture(arch.ArchitectureType(oldArchitecture)) }()

		arch.SetArchitecture(arch.ArchitectureType(opts.Architecture))
	}

	// *sigh* we need to adjust the storeID if its set to "canonical"
	//        because there is no "canonical" store in the store server
	//        it is just ""
	storeID := opts.StoreID
	if storeID == "canonical" {
		storeID = ""
	}

	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir = pwd
	}

	m := store.New(nil, storeID, nil)
	snap, err := m.Snap(name, opts.Channel, false, nil)
	if err != nil {
		return "", fmt.Errorf("failed to find snap: %s", err)
	}
	pb := progress.NewTextProgress()
	tmpName, err := m.Download(name, &snap.DownloadInfo, pb, nil)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpName)

	baseName := filepath.Base(snap.MountFile())
	path := filepath.Join(targetDir, baseName)
	if err := osutil.CopyFile(tmpName, path, 0); err != nil {
		return "", err
	}

	out, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(path+".sideinfo", []byte(out), 0644); err != nil {
		return "", err
	}

	return path, nil
}
