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

type PrepareSerialRequestBehavior struct {
	RegBody map[string]string
}

func MockGadget(c *C, st *state.State, name string, revision snap.Revision, pDBhv *PrepareDeviceBehavior, pSRBhv *PrepareSerialRequestBehavior) (restore func()) {

	sideInfoGadget := &snap.SideInfo{
		RealName: name,
		Revision: revision,
	}

	snapYaml := fmt.Sprintf(`name: %q
type: gadget
version: gadget
`, name)

	if pDBhv != nil || pSRBhv != nil {
		snapYaml += `hooks:
`
	}

	if pDBhv != nil {
		snapYaml += `  prepare-device:
`
	}

	if pSRBhv != nil {
		snapYaml += `  prepare-serial-request:
`
	}

	snaptest.MockSnap(c, snapYaml, sideInfoGadget)
	snapstate.Set(st, name, &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfoGadget}),
		Current:  revision,
	})

	if pDBhv == nil && pSRBhv == nil {
		// nothing to restore
		return func() {}
	}

	// mock the prepare-device and prepare-serial-request hooks

	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		if ctx.HookName() == "prepare-device" {
			// snapctl set the registration params
			_, _, err := ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.url=%q", pDBhv.DeviceSvcURL)}, 0)
			c.Assert(err, IsNil)

			if len(pDBhv.Headers) != 0 {
				h, err := json.Marshal(pDBhv.Headers)
				c.Assert(err, IsNil)
				_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("device-service.headers=%s", string(h))}, 0)
				c.Assert(err, IsNil)
			}

			if pDBhv.ProposedSerial != "" {
				_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.proposed-serial=%q", pDBhv.ProposedSerial)}, 0)
				c.Assert(err, IsNil)
			}

			if len(pDBhv.RegBody) != 0 {
				d, err := yaml.Marshal(pDBhv.RegBody)
				c.Assert(err, IsNil)
				_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.body=%q", d)}, 0)
				c.Assert(err, IsNil)
			}

			return nil, nil
		} else if ctx.HookName() == "prepare-serial-request" {
			// check registration id is present in config
			stdout, _, err := ctlcmd.Run(ctx, []string{"get", "registration.request-id"}, 0)
			c.Assert(err, IsNil)
			c.Assert(string(stdout), Equals, ReqIDPrepareSerialHook+"\n")

			// snapctl set the registration params
			if len(pSRBhv.RegBody) != 0 {
				d, err := json.Marshal(pSRBhv.RegBody)
				c.Assert(err, IsNil)
				_, _, err = ctlcmd.Run(ctx, []string{"set", fmt.Sprintf("registration.body=%q", d)}, 0)
				c.Assert(err, IsNil)
			}

			return nil, nil
		} else {
			return nil, fmt.Errorf("unexpected hook type %q", ctx.HookName())
		}
	})

	return restore
}
