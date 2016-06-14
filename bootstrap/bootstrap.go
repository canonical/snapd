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

package bootstrap

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type headerYaml struct {
	Bootstrap bootstrapYaml `yaml:"bootstrap"`
}

type bootstrapYaml struct {
	Snaps   []string `yaml:"snaps"`
	Rootdir string   `yaml:"rootdir"`
	Channel string   `yaml:"channel"`
	StoreID string   `yaml:"store-id"`
}

func Bootstrap(bootstrapYaml string) error {
	yamlData, err := ioutil.ReadFile(bootstrapYaml)
	if err != nil {
		return err
	}

	var y headerYaml
	err = yaml.Unmarshal(yamlData, &y)
	if err != nil {
		return err
	}

	if y.Bootstrap.Rootdir != "" {
		dirs.SetRootDir(y.Bootstrap.Rootdir)
		defer dirs.SetRootDir("/")
	}

	// sanity check target
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot bootstrap over existing system")
	}

	// put snaps in place
	if err := os.MkdirAll(dirs.SnapBlobDir, 0755); err != nil {
		return err
	}
	opts := &downloadOptions{
		TargetDir: dirs.SnapBlobDir,
		Channel:   y.Bootstrap.Channel,
		StoreID:   y.Bootstrap.StoreID,
	}
	for _, snapName := range y.Bootstrap.Snaps {
		fmt.Printf("Fetching %s\n", snapName)
		if osutil.FileExists(snapName) {
			if err := copyLocalSnapFile(snapName); err != nil {
				return err
			}
		} else {
			if _, err := downloadSnapWithSideInfo(snapName, opts); err != nil {
				return err
			}
		}
	}

	// now do the bootloader stuff
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
		info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
		if err != nil {
			return fmt.Errorf("can not get info for %v", fullname)
		}
		switch info.Type {
		case snap.TypeOS:
			bootvar = "snappy_os"
			bootvar2 = "snappy_good_os"
		case snap.TypeKernel:
			bootvar = "snappy_kernel"
			bootvar2 = "snappy_good_kernel"
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

	if bootloader.Name() == "u-boot" {
		// FIXME: do the equaivalent of extractKernelAssets here
		return fmt.Errorf("IMPLEMENT (or call): extractKernelAssets()")
	}

	return nil
}

func copyLocalSnapFile(snapName string) error {
	snapFile, err := snap.Open(snapName)
	if err != nil {
		return err
	}
	info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
	if err != nil {
		return err
	}
	// local snap gets sideloaded revision
	if info.Revision.Unset() {
		info.Revision = snap.R(-1)
	}
	dst := info.MountFile()

	return osutil.CopyFile(snapName, dst, 0)
}

type downloadOptions struct {
	Series    string
	TargetDir string
	StoreID   string
	Channel   string
}

func downloadSnapWithSideInfo(name string, opts *downloadOptions) (string, error) {
	if opts == nil {
		opts = &downloadOptions{}
	}

	if opts.Series != "" {
		oldSeries := release.Series
		defer func() { release.Series = oldSeries }()

		release.Series = opts.Series
	}

	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir = pwd
	}

	m := store.NewUbuntuStoreSnapRepository(nil, opts.StoreID)
	snap, err := m.Snap(name, opts.Channel, nil)
	if err != nil {
		return "", fmt.Errorf("failed to find snap: %s", err)
	}
	pb := progress.NewTextProgress()
	tmpName, err := m.Download(snap, pb, nil)
	if err != nil {
		return "", err
	}
	baseName := filepath.Base(snap.MountFile())

	path := filepath.Join(targetDir, baseName)
	if err := os.Rename(tmpName, path); err != nil {
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
