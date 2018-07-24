// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package wrappers

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func writeSnapdToolingMountUnit(sysd systemd.Systemd) error {
	content := []byte(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=$PREFIX/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
RequiredBy=snapd.service
`)
	unit := "usr-lib-snapd.mount"
	_, _, err := osutil.EnsureDirState(dirs.SnapServicesDir,
		unit,
		map[string]*osutil.FileState{
			unit: &osutil.FileState{
				Content: content,
				Mode:    0644,
			},
		},
	)
	if err != nil {
		return err
	}
	if err := sysd.Enable(unit); err != nil {
		return err
	}
	if err := sysd.Start(unit); err != nil {
		return err
	}
	return nil
}

func writeSnapdServicesOnCore(s *snap.Info, inter interacter) error {
	// we never write
	if release.OnClassic {
		return nil
	}
	sysd := systemd.New(dirs.GlobalRootDir, inter)

	if err := writeSnapdToolingMountUnit(sysd); err != nil {
		return err
	}

	serviceUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.service"))
	if err != nil {
		return err
	}
	socketUnits, err := filepath.Glob(filepath.Join(s.MountDir(), "lib/systemd/system/*.socket"))
	if err != nil {
		return err
	}
	units := append(serviceUnits, socketUnits...)

	snapdUnits := make(map[string]*osutil.FileState, len(units)+1)
	for _, unit := range units {
		st, err := os.Stat(unit)
		if err != nil {
			return err
		}
		content, err := ioutil.ReadFile(unit)
		if err != nil {
			return err
		}
		re := regexp.MustCompile(`(?m)^ExecStart=(.*)$`)
		content = re.ReplaceAll(content, []byte(fmt.Sprintf(`ExecStart=%s$1`, s.MountDir())))

		snapdUnits[filepath.Base(unit)] = &osutil.FileState{
			Content: content,
			Mode:    st.Mode(),
		}
	}
	changed, removed, err := osutil.EnsureDirStateGlobs(dirs.SnapServicesDir, []string{"snapd.service", "snapd.socket", "snapd.*.service"}, snapdUnits)
	if err != nil {
		// TODO: uhhhh, what do we do in this case?
		return err
	}
	if (len(changed) + len(removed)) == 0 {
		// nothing to do
		return nil
	}
	for _, unit := range removed {
		if err := sysd.Disable(unit); err != nil {
			return err
		}
	}
	for _, unit := range changed {
		if err := sysd.Enable(unit); err != nil {
			return err
		}
	}
	for _, unit := range changed {
		// some units (like the snapd.system-shutdown.service) cannot
		// be started
		if bytes.Contains(snapdUnits[unit].Content, []byte("X-Snapd-Snap: do-not-start")) {
			continue
		}
		if err := sysd.Start(unit); err != nil {
			return err
		}
	}
	if err := sysd.DaemonReload(); err != nil {
		return err
	}

	return nil
}
