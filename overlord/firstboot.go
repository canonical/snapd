// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package overlord

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snappy"
)

func populateStateFromInstalled() error {
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot create state: state %q already exists", dirs.SnapStateFile)
	}

	ovld, err := New()
	if err != nil {
		return err
	}
	ovld.Loop()
	st := ovld.State()

	all, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap"))
	if err != nil {
		return err
	}

	for _, snapPath := range all {
		sf, err := snap.Open(snapPath)
		if err != nil {
			return err
		}

		// Stuff that we do from the first-boot install bits
		var si snap.SideInfo
		metafn := snapPath + ".meta"
		if j, err := ioutil.ReadFile(metafn); err == nil {
			// FIXME: proper error handling
			if err := json.Unmarshal(j, &si); err != nil {
				fmt.Printf("Cannot read metadata: %s %s\n", metafn, err)
				continue
			}
		}
		info, err := snap.ReadInfoFromSnapFile(sf, &si)
		if err != nil {
			return err
		}
		fmt.Printf("Installing %s\n", info.Name())

		st.Lock()
		ts, err := snapstate.InstallPath(st, info.Name(), snapPath, "", 0)
		if err != nil {
			return err
		}

		// FIXME: nuts! short-circut the snap-setup
		tp := ts.Tasks()[0]
		var ss snapstate.SnapSetup
		tp.Get("snap-setup", &ss)
		ss.Revision = si.Revision
		ss.Channel = si.Channel
		ss.SnapID = si.SnapID
		tp.Set("snap-setup", &ss)

		// candiate must be set
		var snapst snapstate.SnapState
		err = snapstate.Get(st, info.Name(), &snapst)
		if err != nil && err != state.ErrNoState {
			return err
		}
		snapst.Candidate = &si
		snapst.Channel = si.Channel
		snapstate.Set(st, info.Name(), &snapst)

		msg := fmt.Sprintf("First boot install of %s", filepath.Base(info.Name()))
		chg := st.NewChange("install-snap", msg)
		chg.AddAll(ts)
		st.Unlock()

		// do it and wait for ready
		st.EnsureBefore(0)
		<-chg.Ready()
		if chg.Status() != state.DoneStatus {
			return fmt.Errorf("cannot run chg: %v", chg)
		}

		// snap.Install() will install them under a new name
		for _, fn := range []string{snapPath, snapPath + ".meta"} {
			if err := os.Remove(fn); err != nil {
				fmt.Printf("Failed to remove %q: %s\n", fn, err)
			}
		}
	}
	ovld.Stop()

	return nil
}

// FirstBoot will do some initial boot setup and then sync the
// state
func FirstBoot() error {
	if err := snappy.FirstBoot(); err != nil {
		return err
	}

	return populateStateFromInstalled()
}
