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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
)

const (
	mntStaticOptions         = "mode=1777,strictatime,nosuid,nodev"
	tmpfsService             = "tmp.mount"
	tmpfsMountPoint          = "/tmp"
	tmpMntServOverrideSubDir = "tmp.mount.d"
	tmpMntServOverrideFile   = "override.conf"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.tmpfs.size"] = true
}

// Regex matches what is specified by tmpfs(5) for the size option
var validTmpfsSize = regexp.MustCompile(`^[0-9]+[kmgKMG%]?$`).MatchString

func validateTmpfsSettings(tr config.ConfGetter) error {
	tmpfsSz, err := coreCfg(tr, "tmpfs.size")
	if err != nil {
		return err
	}
	if tmpfsSz == "" {
		return nil
	}
	if !validTmpfsSize(tmpfsSz) {
		return fmt.Errorf("cannot set tmpfs size %q: invalid size", tmpfsSz)
	}

	return nil
}

func handleTmpfsConfiguration(_ sysconfig.Device, tr config.ConfGetter,
	opts *fsOnlyContext) error {

	tmpfsSz, err := coreCfg(tr, "tmpfs.size")
	if err != nil {
		return err
	}

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
	if tmpfsSz != "" {
		if err := os.MkdirAll(overrDir, 0755); err != nil {
			return err
		}
		options = fmt.Sprintf("%s,size=%s", options, tmpfsSz)
		content := fmt.Sprintf("[Mount]\nOptions=%s\n", options)
		dirContent[tmpMntServOverrideFile] = &osutil.MemoryFileState{
			Content: []byte(content),
			Mode:    0644,
		}
	} else {
		// Use default tmpfs size if empty setting (50%, see tmpfs(5))
		options = fmt.Sprintf("%s,size=50%%", options)
	}
	glob := tmpMntServOverrideFile
	changed, removed, err := osutil.EnsureDirState(overrDir, glob, dirContent)
	if err != nil {
		return err
	}

	// Re-starting the tmp.mount service will fail if some process
	// is using a file in /tmp, so instead of doing that we use
	// the remount option for the mount command, which will not
	// fail in that case. There is however the possibility of a
	// failure in case we are reducing the size to something
	// smaller than the currently used space in the mount. We
	// return an error in that case.
	// XXX if error, the content of override.conf will be different
	// to the setting seen by snapd. But watchdog.go has the same issue??
	if opts == nil && (len(changed) > 0 || len(removed) > 0) {
		output, err := exec.Command("mount", "-o", "remount,"+options, "/tmp").CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot remount tmpfs with new size: %s (%s)",
				err.Error(), output)
		}
	}

	return nil
}
