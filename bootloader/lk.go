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

package bootloader

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type lk struct {
	rootdir       string
	inRuntimeMode bool
}

// newLk create a new lk bootloader object
func newLk(rootdir string, opts *Options) Bootloader {
	l := &lk{rootdir: rootdir}

	// XXX: in the long run we want this to go away, we probably add
	//      something like "boot.PrepareImage()" and add an (optional)
	//      method "PrepareImage" to the bootloader interface that is
	//      used to setup a bootloader from prepare-image if things
	//      are very different from runtime vs image-building mode.
	//
	// determine mode we are in, runtime or image build
	l.inRuntimeMode = !opts.PrepareImageTime

	if !osutil.FileExists(l.envFile()) {
		return nil
	}

	return l
}

func (l *lk) setRootDir(rootdir string) {
	l.rootdir = rootdir
}

func (l *lk) Name() string {
	return "lk"
}

func (l *lk) dir() string {
	// we have two scenarios, image building and runtime
	// during image building we store environment into file
	// at runtime environment is written directly into dedicated partition
	if l.inRuntimeMode {
		return filepath.Join(l.rootdir, "/dev/disk/by-partlabel/")
	} else {
		return filepath.Join(l.rootdir, "/boot/lk/")
	}
}

func (l *lk) InstallBootConfig(gadgetDir string) (bool, error) {
	gadgetFile := filepath.Join(gadgetDir, l.Name()+".conf")
	systemFile := l.ConfigFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (l *lk) ConfigFile() string {
	return l.envFile()
}

func (l *lk) envFile() string {
	// as for dir, we have two scenarios, image building and runtime
	if l.inRuntimeMode {
		// TO-DO: this should be eventually fetched from gadget.yaml
		return filepath.Join(l.dir(), "snapbootsel")
	} else {
		return filepath.Join(l.dir(), "snapbootsel.bin")
	}
}

func (l *lk) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env := lkenv.NewEnv(l.envFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (l *lk) SetBootVars(values map[string]string) error {
	env := lkenv.NewEnv(l.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}

	// update environment only if something change
	dirty := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirty = true
	}

	if dirty {
		return env.Save()
	}

	return nil
}

// ExtractKernelAssets extract kernel assets per bootloader specifics
// lk bootloader requires boot partition to hold valid boot image
// there are two boot partition available, one holding current bootimage
// kernel assets are extracted to other (free) boot partition
// in case this function is called as part of image creation,
// boot image is extracted to the file
func (l *lk) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	blobName := filepath.Base(s.MountFile())

	logger.Debugf("ExtractKernelAssets (%s)", blobName)

	env := lkenv.NewEnv(l.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}

	bootPartition, err := env.FindFreeBootPartition(blobName)
	if err != nil {
		return err
	}

	if l.inRuntimeMode {
		logger.Debugf("ExtractKernelAssets handling run time usecase")
		// this is live system, extracted bootimg needs to be flashed to
		// free bootimg partition and env has to be updated with
		// new kernel snap to bootimg partition mapping
		tmpdir, err := ioutil.TempDir("", "bootimg")
		if err != nil {
			return fmt.Errorf("cannot create temp directory: %v", err)
		}
		defer os.RemoveAll(tmpdir)

		bootImg := env.GetBootImageName()
		if err := snapf.Unpack(bootImg, tmpdir); err != nil {
			return fmt.Errorf("cannot unpack %s: %v", bootImg, err)
		}
		// write boot.img to free boot partition
		bootimgName := filepath.Join(tmpdir, bootImg)
		bif, err := os.Open(bootimgName)
		if err != nil {
			return fmt.Errorf("cannot open unpacked %s: %v", bootImg, err)
		}
		defer bif.Close()
		bpart := filepath.Join(l.dir(), bootPartition)

		bpf, err := os.OpenFile(bpart, os.O_WRONLY, 0660)
		if err != nil {
			return fmt.Errorf("cannot open boot partition [%s]: %v", bpart, err)
		}
		defer bpf.Close()

		if _, err := io.Copy(bpf, bif); err != nil {
			return err
		}
	} else {
		// we are preparing image, just extract boot image to bootloader directory
		logger.Debugf("ExtractKernelAssets handling image prepare")
		if err := snapf.Unpack(env.GetBootImageName(), l.dir()); err != nil {
			return fmt.Errorf("cannot open unpacked %s: %v", env.GetBootImageName(), err)
		}
	}
	if err := env.SetBootPartition(bootPartition, blobName); err != nil {
		return err
	}

	return env.Save()
}

func (l *lk) RemoveKernelAssets(s snap.PlaceInfo) error {
	blobName := filepath.Base(s.MountFile())
	logger.Debugf("RemoveKernelAssets (%s)", blobName)
	env := lkenv.NewEnv(l.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	dirty, _ := env.FreeBootPartition(blobName)
	if dirty {
		return env.Save()
	}
	return nil
}
