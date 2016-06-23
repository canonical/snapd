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
	"github.com/snapcore/snapd/firstboot"
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
	"snap-environment": true,
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
func checkSnap(st *state.State, snapFilePath string, curInfo *snap.Info, flags Flags) error {
	// XXX: actually verify snap before using content from it unless dev-mode

	s, _, err := openSnapFile(snapFilePath, nil)
	if err != nil {
		return err
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

	if s.Type != snap.TypeGadget {
		return nil
	}

	// gadget specific checks
	if release.OnClassic {
		// for the time being
		return fmt.Errorf("cannot install a gadget snap on classic")
	}

	st.Lock()
	defer st.Unlock()
	currentGadget, err := GadgetInfo(st)
	// in firstboot we have no gadget yet - that is ok
	if err == state.ErrNoState && !firstboot.HasRun() {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find original gadget snap")
	}

	// TODO: actually compare snap ids, from current gadget and candidate
	if currentGadget.Name() != s.Name() {
		return fmt.Errorf("cannot replace gadget snap with a different one")
	}

	return nil
}
