// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package configcore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
)

const (
	mntStaticOptions         = "mode=1777,strictatime,nosuid,nodev"
	tmpfsMountPoint          = "/tmp"
	tmpMntServOverrideSubDir = "tmp.mount.d"
	tmpMntServOverrideFile   = "override.conf"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.tmp.size"] = true
}

func validTmpfsSize(sizeStr string) error {
	if sizeStr == "" {
		return nil
	}

	// TODO allow also percentages. That is allowed for CPU quotas so
	// it is probably fine to allow that for tmp.size too.
	size := mylog.Check2(quantity.ParseSize(sizeStr))

	// Do not allow less than 16mb
	// 0 is special and means unlimited
	if size > 0 && size < 16*quantity.SizeMiB {
		return fmt.Errorf("size is less than 16Mb")
	}

	return nil
}

func validateTmpfsSettings(tr ConfGetter) error {
	tmpfsSz := mylog.Check2(coreCfg(tr, "tmp.size"))

	return validTmpfsSize(tmpfsSz)
}

func handleTmpfsConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	tmpfsSz := mylog.Check2(coreCfg(tr, "tmp.size"))

	// Create override configuration file for tmp.mount service

	// Create /etc/systemd/system/tmp.mount.d if needed
	var overrDir string
	if opts == nil {
		// runtime system
		overrDir = dirs.SnapServicesDir
	} else {
		overrDir = dirs.SnapServicesDirUnder(opts.RootDir)
	}
	overrDir = filepath.Join(overrDir, tmpMntServOverrideSubDir)

	// Write service config override if needed
	options := mntStaticOptions
	dirContent := make(map[string]osutil.FileState, 1)
	cfgFilePath := filepath.Join(overrDir, tmpMntServOverrideFile)
	modify := true
	if tmpfsSz != "" {
		mylog.Check(os.MkdirAll(overrDir, 0755))

		options = fmt.Sprintf("%s,size=%s", options, tmpfsSz)
		content := fmt.Sprintf("[Mount]\nOptions=%s\n", options)
		dirContent[tmpMntServOverrideFile] = &osutil.MemoryFileState{
			Content: []byte(content),
			Mode:    0644,
		}
		oldContent := mylog.Check2(os.ReadFile(cfgFilePath))
		if err == nil && content == string(oldContent) {
			modify = false
		}
	} else {
		// Use default tmpfs size if empty setting (50%, see tmpfs(5))
		options = fmt.Sprintf("%s,size=50%%", options)
		// In this case, we are removing the file, so we will
		// not do anything if the file is not there alreay.
		if _ := mylog.Check2(os.Stat(cfgFilePath)); errors.Is(err, os.ErrNotExist) {
			modify = false
		}
	}

	// Re-starting the tmp.mount service will fail if some process
	// is using a file in /tmp, so instead of doing that we use
	// the remount option for the mount command, which will not
	// fail in that case. There is however the possibility of a
	// failure in case we are reducing the size to something
	// smaller than the currently used space in the mount. We
	// return an error in that case.
	if opts == nil && modify {
		if output := mylog.Check2(exec.Command("mount", "-o", "remount,"+options, tmpfsMountPoint).CombinedOutput()); err != nil {
			return fmt.Errorf("cannot remount tmpfs with new size: %s (%s)", err.Error(), output)
		}
	}

	glob := tmpMntServOverrideFile
	_, _ := mylog.Check3(osutil.EnsureDirState(overrDir, glob, dirContent))

	return nil
}
