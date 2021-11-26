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
	"path/filepath"
	"regexp"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

const (
	tmpfsService             = "tmp.mount"
	tmpfsMountPoint          = "/tmp"
	tmpMntServOverrideSubDir = "tmp.mount.d"
	tmpMntServOverrideFile   = "override.conf"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.tmpfs.size"] = true
}

var debugPrint = fmt.Printf

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
	debugPrint("validateTmpfsSettings: %q\n", tmpfsSz)
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
	debugPrint("handleTmpfsConfiguration: %q\n", tmpfsSz)

	// Create override configuration file for tmp.mount service

	// Create /etc/systemd/system/tmp.mount.d if needed
	var overrDir string
	var sysd systemd.Systemd
	if opts == nil {
		// runtime system
		overrDir = dirs.SnapServicesDir
		sysd = systemd.NewUnderRoot(dirs.GlobalRootDir,
			systemd.SystemMode, &sysdLogger{})
	} else {
		overrDir = dirs.SnapServicesDirUnder(opts.RootDir)
	}
	overrDir = filepath.Join(overrDir, tmpMntServOverrideSubDir)

	// Write service config override if needed
	// TODO check options
	// These come from /usr/share/systemd/tmp.mount
	// But we actually have rw,relatime as default in UC??
	// stat /tmp/ -> shows 1777
	// strictatime seems activated
	// ...but nodev is not -> mknod vda, change permissions, can fdisk with normal user
	// if nodev is set this should not be possible
	// and nosuid is not being applied either (checked by copying there sudo command)
	dirContent := make(map[string]osutil.FileState, 1)
	if tmpfsSz != "" {
		if err := os.MkdirAll(overrDir, 0755); err != nil {
			return err
		}
		content := fmt.Sprintf("[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=%s\n",
			tmpfsSz)
		dirContent[tmpMntServOverrideFile] = &osutil.MemoryFileState{
			Content: []byte(content),
			Mode:    0644,
		}
	}
	glob := tmpMntServOverrideFile
	changed, removed, err := osutil.EnsureDirState(overrDir, glob, dirContent)
	if err != nil {
		return err
	}

	// TODO What happens if we are reducing the size??
	if sysd != nil && (len(changed) > 0 || len(removed) > 0) {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
		return sysd.Restart(tmpfsService, 60*time.Second)
	}

	return nil
}
