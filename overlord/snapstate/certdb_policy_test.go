// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func makeModelWithBootBaseAndChannel(base, defaultChannel string) *asserts.Model {
	headers := map[string]any{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "amd64",
		"base":         base,
		"grade":        "dangerous",
		"timestamp":    "2018-01-01T08:00:00+00:00",
		"snaps": []any{
			map[string]any{
				"name":            "kernel",
				"id":              snaptest.AssertedSnapID("kernel"),
				"type":            "kernel",
				"default-channel": "20/stable",
			},
			map[string]any{
				"name":            "brand-gadget",
				"id":              snaptest.AssertedSnapID("brand-gadget"),
				"type":            "gadget",
				"default-channel": "20/stable",
			},
			map[string]any{
				"name":            base,
				"id":              snaptest.AssertedSnapID(base),
				"type":            "base",
				"default-channel": defaultChannel,
			},
		},
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Model)
}

func (s *snapmgrTestSuite) TestShouldScheduleUpdateCertDBForRefresh(c *C) {
	modelBaseCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: ModelWithBase("core18")}
	remodelCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: ModelWithBase("core18"), Remodeling: true}
	classicCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModelClassicWithModes("pc", nil)}

	tests := []struct {
		name         string
		ctx          snapstate.DeviceContext
		isRefresh    bool
		snapType     snap.Type
		instanceName string
		expected     bool
	}{
		{name: "nil device context", ctx: nil, isRefresh: true, snapType: snap.TypeBase, instanceName: "core18", expected: false},
		{name: "not a refresh", ctx: modelBaseCtx, isRefresh: false, snapType: snap.TypeBase, instanceName: "core18", expected: false},
		{name: "remodel refresh path", ctx: remodelCtx, isRefresh: true, snapType: snap.TypeBase, instanceName: "core18", expected: false},
		{name: "non-base snap", ctx: modelBaseCtx, isRefresh: true, snapType: snap.TypeApp, instanceName: "core18", expected: false},
		{name: "classic model", ctx: classicCtx, isRefresh: true, snapType: snap.TypeBase, instanceName: "core22", expected: false},
		{name: "non-model base", ctx: modelBaseCtx, isRefresh: true, snapType: snap.TypeBase, instanceName: "some-base", expected: false},
		{name: "model base", ctx: modelBaseCtx, isRefresh: true, snapType: snap.TypeBase, instanceName: "core18", expected: true},
	}

	for _, tc := range tests {
		c.Check(snapstate.ShouldScheduleUpdateCertDBForRefresh(snapstate.UpdateCertDBForRefreshOptions{
			DeviceCtx:    tc.ctx,
			IsRefresh:    tc.isRefresh,
			SnapType:     tc.snapType,
			InstanceName: tc.instanceName,
		}), Equals, tc.expected, Commentf(tc.name))
	}
}

func (s *snapmgrTestSuite) TestShouldScheduleUpdateCertDBForModelChange(c *C) {
	current := makeModelWithBootBaseAndChannel("core20", "20/stable")
	same := makeModelWithBootBaseAndChannel("core20", "20/stable")
	trackChanged := makeModelWithBootBaseAndChannel("core20", "22/stable")
	baseChanged := makeModelWithBootBaseAndChannel("core22", "22/stable")

	tests := []struct {
		name     string
		current  *asserts.Model
		next     *asserts.Model
		expected bool
	}{
		{name: "nil current", current: nil, next: same, expected: false},
		{name: "nil new", current: current, next: nil, expected: false},
		{name: "same boot base", current: current, next: same, expected: false},
		{name: "boot base track changed", current: current, next: trackChanged, expected: true},
		{name: "boot base name changed", current: current, next: baseChanged, expected: true},
	}

	for _, tc := range tests {
		c.Check(snapstate.ShouldScheduleUpdateCertDBForModelChange(tc.current, tc.next), Equals, tc.expected, Commentf(tc.name))
	}
}
