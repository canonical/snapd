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
	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snappy"
)

func populateStateFromInstalled() error {
	all, err := (&snappy.Overlord{}).Installed()
	if err != nil {
		return err
	}

	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot create state: state %q already exists", dirs.SnapStateFile)
	}

	st := state.New(&overlordStateBackend{
		path: dirs.SnapStateFile,
	})
	st.Lock()
	defer st.Unlock()

	for _, sn := range all {
		// no need to do a snapstate.Get() because this is firstboot
		info := sn.Info()

		var snapst snapstate.SnapState
		snapst.Sequence = append(snapst.Sequence, &info.SideInfo)
		snapst.Channel = info.Channel
		snapst.Active = sn.IsActive()
		snapstate.Set(st, sn.Name(), &snapst)
	}

	return nil
}

// FIXME:
// This is not the final way we will do the state sync. This is just
// an intermediate step to have working images again. We need to
// figure out how we want first-boot to look like.
func FirstBoot() error {
	if err := snappy.FirstBoot(); err != nil {
		return err
	}

	return populateStateFromInstalled()
}
