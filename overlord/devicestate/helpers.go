// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package devicestate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func setDeviceFromModelAssertion(st *state.State, device *auth.DeviceState, model *asserts.Model) error {
	device.Brand = model.BrandID()
	device.Model = model.Model()
	return internal.SetDevice(st, device)
}

func gadgetDataFromInfo(info *snap.Info, model *asserts.Model) (*gadget.GadgetData, error) {
	// we do not perform consistency validation here because that
	// has been done when the gadget was installed for
	// current/already local revisions, or in the check-snap task
	// for incoming gadgets.
	gi := mylog.Check2(gadget.ReadInfo(info.MountDir(), model))

	return &gadget.GadgetData{Info: gi, RootDir: info.MountDir()}, nil
}

var systemForPreseeding = func() (label string, err error) {
	systemLabels := mylog.Check2(filepath.Glob(filepath.Join(dirs.SnapSeedDir, "systems", "*")))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot list available systems: %v", err)
	}
	if len(systemLabels) == 0 {
		return "", fmt.Errorf("no system to preseed")
	}
	if len(systemLabels) > 1 {
		return "", fmt.Errorf("expected a single system for preseeding, found %d", len(systemLabels))
	}
	return filepath.Base(systemLabels[0]), nil
}
