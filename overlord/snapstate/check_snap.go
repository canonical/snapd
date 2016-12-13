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
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
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

func checkAssumes(si *snap.Info) error {
	missing := ([]string)(nil)
	for _, flag := range si.Assumes {
		if strings.HasPrefix(flag, "snapd") && checkVersion(flag[5:]) {
			continue
		}
		if !featureSet[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		hint := "try to refresh the core snap"
		if release.OnClassic {
			hint = "try to update snapd and refresh the core snap"
		}
		return fmt.Errorf("snap %q assumes unsupported features: %s (%s)", si.Name(), strings.Join(missing, ", "), hint)
	}
	return nil
}

var versionExp = regexp.MustCompile(`^([1-9][0-9]*)(?:\.([0-9]+)(?:\.([0-9]+))?)?`)

func checkVersion(version string) bool {
	req := versionExp.FindStringSubmatch(version)
	if req == nil || req[0] != version {
		return false
	}

	if cmd.Version == "unknown" {
		return true // Development tree.
	}

	cur := versionExp.FindStringSubmatch(cmd.Version)
	if cur == nil {
		return false
	}

	for i := 1; i < len(req); i++ {
		if req[i] == "" {
			return true
		}
		if cur[i] == "" {
			return false
		}
		reqN, err1 := strconv.Atoi(req[i])
		curN, err2 := strconv.Atoi(cur[i])
		if err1 != nil || err2 != nil {
			panic("internal error: version regexp is broken")
		}
		if curN != reqN {
			return curN > reqN
		}
	}

	return true
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
	if s.NeedsClassic() {
		if !release.OnClassic {
			return fmt.Errorf("snap %q requires classic confinement which is only available on classic systems", s.Name())
		}
		if !flags.Classic {
			return fmt.Errorf("snap %q requires consent to use classic confinement", s.Name())
		}
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
type CheckSnapCallback func(st *state.State, snap, curSnap *snap.Info, flags Flags) error

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

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	kind := ""
	var currentInfo func(*state.State) (*snap.Info, error)
	switch snapInfo.Type {
	case snap.TypeGadget:
		kind = "gadget"
		currentInfo = GadgetInfo
	case snap.TypeKernel:
		kind = "kernel"
		currentInfo = KernelInfo
	default:
		// not a relevant check
		return nil
	}

	if release.OnClassic {
		// for the time being
		return fmt.Errorf("cannot install a %s snap on classic", kind)
	}

	currentSnap, err := currentInfo(st)
	// in firstboot we have no gadget/kernel yet - that is ok
	// devicestate considers that case
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find original %s snap: %v", kind, err)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID != "" {
		if currentSnap.SnapID == snapInfo.SnapID {
			// same snap
			return nil
		}
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID == "" {
		return fmt.Errorf("cannot replace signed %s snap with an unasserted one", kind)
	}

	if currentSnap.Name() != snapInfo.Name() {
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	return nil
}

func init() {
	AddCheckSnapCallback(checkCoreName)
	AddCheckSnapCallback(checkGadgetOrKernel)
}
