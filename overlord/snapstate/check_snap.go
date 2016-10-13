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

package snapstate

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// featureSet contains the flag values that can be listed in assumes entries
// that this ubuntu-core actually provides.
var featureSet = map[string]bool{
	// Support for common data directory across revisions of a snap.
	"common-data-dir": true,
	// Support for the "Environment:" feature in snap.yaml
	"snap-env": true,
}

func checkAssumes(s *snap.Info) error {
	missing := ([]string)(nil)
	for _, flag := range s.Assumes {
		if !featureSet[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("snap %q assumes unsupported features: %s (try new ubuntu-core)", s.Name(), strings.Join(missing, ", "))
	}
	return nil
}

var openSnapFile = backend.OpenSnapFile

// checkSnap ensures that the snap can be installed.
func checkSnap(st *state.State, snapFilePath string, si *snap.SideInfo, curInfo *snap.Info, flags Flags) error {
	// This assumes that the snap was already verified or --dangerous was used.

	s, _, err := openSnapFile(snapFilePath, si)
	if err != nil {
		return err
	}

	if s.NeedsDevMode() && !flags.DevModeAllowed() {
		return fmt.Errorf("snap %q requires devmode or confinement override", s.Name())
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.Architectures) {
		return fmt.Errorf("snap %q supported architectures (%s) are incompatible with this system (%s)", s.Name(), strings.Join(s.Architectures, ", "), arch.UbuntuArchitecture())
	}

	// check assumes
	err = checkAssumes(s)
	if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	for _, check := range checkSnapCallbacks {
		err := check(st, s, curInfo, flags)
		if err != nil {
			return err
		}
	}

	return nil
}

// CheckSnapCallback defines callbacks for checking a snap for installation or refresh.
type CheckSnapCallback func(s *state.State, snap, curSnap *snap.Info, flags Flags) error

var checkSnapCallbacks []CheckSnapCallback

// AddCheckSnapCallback installs a callback to check a snap for installation or refresh.
func AddCheckSnapCallback(check CheckSnapCallback) {
	checkSnapCallbacks = append(checkSnapCallbacks, check)
}

func MockCheckSnapCallbacks(checks []CheckSnapCallback) (restore func()) {
	prev := checkSnapCallbacks
	checkSnapCallbacks = checks
	return func() {
		checkSnapCallbacks = prev
	}
}

func checkCoreName(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	if snapInfo.Type != snap.TypeOS {
		// not a relevant check
		return nil
	}
	if curInfo != nil {
		// already one of these installed
		return nil
	}
	core, err := CoreInfo(st)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	if core.Name() != snapInfo.Name() {
		return fmt.Errorf("cannot install core snap %q when core snap %q is already present", snapInfo.Name(), core.Name())
	}

	return nil
}

func checkGadget(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	if snapInfo.Type != snap.TypeGadget {
		// not a relevant check
		return nil
	}
	if release.OnClassic {
		// for the time being
		return fmt.Errorf("cannot install a gadget snap on classic")
	}

	currentGadget, err := GadgetInfo(st)
	// in firstboot we have no gadget yet - that is ok
	// devicestate considers that case
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find original gadget snap: %v", err)
	}

	if currentGadget.SnapID != "" && snapInfo.SnapID != "" {
		if currentGadget.SnapID == snapInfo.SnapID {
			// same snap
			return nil
		}
		return fmt.Errorf("cannot replace gadget snap with a different one")
	}

	if currentGadget.Name() != snapInfo.Name() {
		return fmt.Errorf("cannot replace gadget snap with a different one")
	}

	return nil
}

func init() {
	AddCheckSnapCallback(checkCoreName)
	AddCheckSnapCallback(checkGadget)
}
