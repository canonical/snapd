// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

func init() {
	patches[7] = patch7
}

type patch7AuthState struct {
	Device *patch7DeviceState `json:"device,omitempty"`
}

type patch7DeviceState struct {
	Brand string `json:"brand,omitempty"`
	Model string `json:"model,omitempty"`
}

// patch7:
//  - on all-snaps mark required snaps as such in snap state
func patch7(st *state.State) error {
	if release.OnClassic {
		// nothing to do
		return nil
	}

	var stateMap map[string]map[string]interface{}
	err := st.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	var auth patch7AuthState
	err = st.Get("auth", &auth)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	if auth.Device == nil || auth.Device.Brand == "" || auth.Device.Model == "" {
		// nothing to do
		return nil
	}

	db, err := sysdb.Open()
	if err != nil {
		return err
	}

	a, err := db.Find(asserts.ModelType, map[string]string{
		"series":   release.Series,
		"brand-id": auth.Device.Brand,
		"model":    auth.Device.Model,
	})
	if err == asserts.ErrNotFound {
		// nothing to do
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find model assertion: %v", err)
	}

	model := a.(*asserts.Model)

	required := model.RequiredSnaps()

	for name, snapst := range stateMap {
		for _, req := range required {
			if req == name {
				snapst["required"] = true
				break
			}
		}
	}

	st.Set("snaps", stateMap)

	return nil
}
