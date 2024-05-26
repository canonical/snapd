// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package devicestatetest

import (
	"encoding/json"
	"fmt"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type PrepareDeviceBehavior struct {
	DeviceSvcURL   string
	Headers        map[string]string
	RegBody        map[string]string
	ProposedSerial string
}

func MockGadget(c *C, st *state.State, name string, revision snap.Revision, pDBhv *PrepareDeviceBehavior) (restore func()) {
	sideInfoGadget := &snap.SideInfo{
		RealName: name,
		Revision: revision,
	}

	snapYaml := fmt.Sprintf(`name: %q
type: gadget
version: gadget
`, name)

	if pDBhv != nil {
		snapYaml += `hooks:
  prepare-device:
`
	}

	snaptest.MockSnap(c, snapYaml, sideInfoGadget)
	snapstate.Set(st, name, &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfoGadget}),
		Current:  revision,
	})

	if pDBhv == nil {
		// nothing to restore
		return func() {}
	}

	// mock the prepare-device hook

	return hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "prepare-device")

		// snapctl set the registration params
		_, _ := mylog.Check3(ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.url=%q", pDBhv.DeviceSvcURL)}, 0))


		if len(pDBhv.Headers) != 0 {
			h := mylog.Check2(json.Marshal(pDBhv.Headers))

			_, _ = mylog.Check3(ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.headers=%s", string(h))}, 0))

		}

		if pDBhv.ProposedSerial != "" {
			_, _ = mylog.Check3(ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.proposed-serial=%q", pDBhv.ProposedSerial)}, 0))

		}

		if len(pDBhv.RegBody) != 0 {
			d := mylog.Check2(yaml.Marshal(pDBhv.RegBody))

			_, _ = mylog.Check3(ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.body=%q", d)}, 0))

		}

		return nil, nil
	})
}
