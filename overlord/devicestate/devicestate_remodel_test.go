// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

package devicestate_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

type deviceMgrRemodelSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrRemodelSuite{})

func (s *deviceMgrRemodelSuite) SetUpTest(c *C) {
	classic := false
	s.setupBaseTest(c, classic)
	snapstate.EnforceLocalValidationSets = assertstate.ApplyLocalEnforcedValidationSets
}

func (s *deviceMgrRemodelSuite) TestRemodelUnhappyNotSeeded(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", false)

	newModel := s.brands.Model("canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	_, err := devicestate.Remodel(s.state, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, "cannot remodel until fully seeded")
}

func (s *deviceMgrRemodelSuite) TestRemodelSnapdBasedToCoreBased(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()
	s.state.Set("seeded", true)

	model := s.brands.Model("canonical", "my-model", modelDefaults, map[string]interface{}{
		"base": "core18",
	})

	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})

	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	s.makeSerialAssertionInState(c, "canonical", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("canonical", "my-model", modelDefaults, map[string]interface{}{
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, `cannot remodel from UC18\+ \(using snapd snap\) system back to UC16 system \(using core snap\)`)
	c.Assert(chg, IsNil)
}

var mockCore20ModelHeaders = map[string]interface{}{
	"brand":        "canonical",
	"model":        "pc-model-20",
	"architecture": "amd64",
	"grade":        "dangerous",
	"base":         "core20",
	"snaps":        mockCore20ModelSnaps,
}

var mockCore20ModelSnaps = []interface{}{
	map[string]interface{}{
		"name":            "pc-kernel",
		"id":              "pckernelidididididididididididid",
		"type":            "kernel",
		"default-channel": "20",
	},
	map[string]interface{}{
		"name":            "pc",
		"id":              "pcididididididididididididididid",
		"type":            "gadget",
		"default-channel": "20",
	},
}

// copy current model unless new model test data is different
// and delete nil keys in new model
func mergeMockModelHeaders(cur, new map[string]interface{}) {
	for k, v := range cur {
		if v, ok := new[k]; ok {
			if v == nil {
				delete(new, k)
			}
			continue
		}
		new[k] = v
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := map[string]interface{}{
		"brand":        "canonical",
		"model":        "pc-model",
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	s.makeModelAssertionInState(c, cur["brand"].(string), cur["model"].(string), map[string]interface{}{
		"architecture": cur["architecture"],
		"kernel":       cur["kernel"],
		"gadget":       cur["gadget"],
	})
	s.makeSerialAssertionInState(c, cur["brand"].(string), cur["model"].(string), "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  cur["brand"].(string),
		Model:  cur["model"].(string),
		Serial: "orig-serial",
	})

	// ensure all error cases are checked
	for _, t := range []struct {
		new    map[string]interface{}
		errStr string
	}{
		{map[string]interface{}{"architecture": "pdp-7"}, "cannot remodel to different architectures yet"},
		{map[string]interface{}{"base": "core18"}, "cannot remodel from core to bases yet"},
		// pre-UC20 to UC20
		{map[string]interface{}{"base": "core20", "kernel": nil, "gadget": nil, "snaps": mockCore20ModelSnaps}, `cannot remodel from pre-UC20 to UC20\+ models`},
		{map[string]interface{}{"base": "core20", "kernel": nil, "gadget": nil, "classic": "true", "distribution": "ubuntu", "snaps": mockCore20ModelSnaps}, `cannot remodel across classic and non-classic models`},
	} {
		mergeMockModelHeaders(cur, t.new)
		new := s.brands.Model(t.new["brand"].(string), t.new["model"].(string), t.new)
		chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
		c.Check(chg, IsNil)
		c.Check(err, ErrorMatches, t.errStr)
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelFromClassicUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := map[string]interface{}{
		"brand":        "canonical",
		"model":        "pc-model",
		"architecture": "amd64",
		"classic":      "true",
		"gadget":       "pc",
	}
	s.makeModelAssertionInState(c, cur["brand"].(string), cur["model"].(string), map[string]interface{}{
		"architecture": cur["architecture"],
		"gadget":       cur["gadget"],
		"classic":      cur["classic"],
	})
	s.makeSerialAssertionInState(c, cur["brand"].(string), cur["model"].(string), "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  cur["brand"].(string),
		Model:  cur["model"].(string),
		Serial: "orig-serial",
	})

	new := s.brands.Model(cur["brand"].(string), "new-model", map[string]interface{}{
		"architecture": cur["architecture"],
		"gadget":       cur["gadget"],
		"classic":      cur["classic"],
	})

	_, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(err, ErrorMatches, `cannot remodel from classic \(non-hybrid\) model`)
}

func (s *deviceMgrRemodelSuite) TestRemodelCheckGrade(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := mockCore20ModelHeaders
	s.makeModelAssertionInState(c, cur["brand"].(string), cur["model"].(string), map[string]interface{}{
		"architecture": cur["architecture"],
		"base":         cur["base"],
		"grade":        cur["grade"],
		"snaps":        cur["snaps"],
	})
	s.makeSerialAssertionInState(c, cur["brand"].(string), cur["model"].(string), "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  cur["brand"].(string),
		Model:  cur["model"].(string),
		Serial: "orig-serial",
	})

	// ensure all error cases are checked
	for idx, t := range []struct {
		new    map[string]interface{}
		errStr string
	}{
		// uc20 model
		{map[string]interface{}{"grade": "signed"}, "cannot remodel from grade dangerous to grade signed"},
		{map[string]interface{}{"grade": "secured"}, "cannot remodel from grade dangerous to grade secured"},
		// non-uc20 model
		{map[string]interface{}{"snaps": nil, "grade": nil, "base": "core", "gadget": "pc", "kernel": "pc-kernel"}, "cannot remodel from grade dangerous to grade unset"},
	} {
		c.Logf("tc: %v", idx)
		mergeMockModelHeaders(cur, t.new)
		new := s.brands.Model(t.new["brand"].(string), t.new["model"].(string), t.new)
		chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
		c.Check(chg, IsNil)
		c.Check(err, ErrorMatches, t.errStr)
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelCannotUseOldModel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := map[string]interface{}{
		"brand":        "canonical",
		"model":        "pc-model",
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "2",
	})
	// no serial assertion, no serial in state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	newModelHdrs := map[string]interface{}{
		"revision": "1",
	}
	mergeMockModelHeaders(cur, newModelHdrs)
	new := s.brands.Model("canonical", "pc-model", newModelHdrs)
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(chg, IsNil)
	c.Check(err, ErrorMatches, "cannot remodel to older revision 1 of model canonical/pc-model than last revision 2 known to the device")
}

func (s *deviceMgrRemodelSuite) TestRemodelRequiresSerial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	cur := map[string]interface{}{
		"brand":        "canonical",
		"model":        "pc-model",
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	// no serial assertion, no serial in state
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	newModelHdrs := map[string]interface{}{
		"revision": "2",
	}
	mergeMockModelHeaders(cur, newModelHdrs)
	new := s.brands.Model("canonical", "pc-model", newModelHdrs)
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(chg, IsNil)
	c.Check(err, ErrorMatches, "cannot remodel without a serial")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchGadgetTrack(c *C) {
	s.testRemodelTasksSwitchTrack(c, "pc", map[string]interface{}{
		"gadget": "pc=18",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchKernelTrack(c *C) {
	s.testRemodelTasksSwitchTrack(c, "pc-kernel", map[string]interface{}{
		"kernel": "pc-kernel=18",
	})
}

func (s *deviceMgrRemodelSuite) testRemodelTasksSwitchTrack(c *C, whatRefreshes string, newModelOverrides map[string]interface{}) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testDeviceCtx snapstate.DeviceContext

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")
		c.Check(name, Equals, whatRefreshes)
		c.Check(opts.Channel, Equals, "18")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	headers := map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	}
	for k, v := range newModelOverrides {
		headers[k] = v
	}
	new := s.brands.Model("canonical", "pc-model", headers)

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true, DeviceModel: new, OldDeviceModel: current}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	// 2 snaps, plus one track switch plus the remodel task, the
	// wait chain is tested in TestRemodel*
	c.Assert(tss, HasLen, 4)
}

func createLocalSnap(c *C, name, id string, revision int, snapType string, base string, files [][]string) (*snap.SideInfo, string) {
	yaml := fmt.Sprintf(`name: %s
version: 1.0
epoch: 1
`, name)

	if snapType != "" {
		yaml += fmt.Sprintf("\ntype: %s\n", snapType)
	}

	if base != "" {
		yaml += fmt.Sprintf("\nbase: %s\n", base)
	}

	si := &snap.SideInfo{
		RealName: name,
		Revision: snap.R(revision),
		SnapID:   id,
	}
	tmpPath := snaptest.MakeTestSnapWithFiles(c, yaml, files)
	return si, tmpPath
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchGadget(c *C) {
	newTrack := map[string]string{"other-gadget": "18"}
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{"gadget": "other-gadget=18"}, nil, nil, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchLocalGadget(c *C) {
	newTrack := map[string]string{"other-gadget": "18"}
	sis := make([]*snap.SideInfo, 1)
	paths := make([]string, 1)
	sis[0], paths[0] = createLocalSnap(c, "pc", "pcididididididididididididididid", 3, "gadget", "", nil)
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{"gadget": "other-gadget=18"},
		sis, paths, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchKernel(c *C) {
	newTrack := map[string]string{"other-kernel": "18"}
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{"kernel": "other-kernel=18"}, nil, nil, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchLocalKernel(c *C) {
	newTrack := map[string]string{"other-kernel": "18"}
	sis := make([]*snap.SideInfo, 1)
	paths := make([]string, 1)
	sis[0], paths[0] = createLocalSnap(c, "pc-kernel", "pckernelidididididididididididid", 3, "kernel", "", nil)
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{"kernel": "other-kernel=18"},
		sis, paths, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchKernelAndGadget(c *C) {
	newTrack := map[string]string{"other-kernel": "18", "other-gadget": "18"}
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{
			"kernel": "other-kernel=18",
			"gadget": "other-gadget=18"}, nil, nil, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchLocalKernelAndGadget(c *C) {
	newTrack := map[string]string{"other-kernel": "18", "other-gadget": "18"}
	sis := make([]*snap.SideInfo, 2)
	paths := make([]string, 2)
	sis[0], paths[0] = createLocalSnap(c, "pc-kernel", "pckernelidididididididididididid", 3, "kernel", "", nil)
	sis[1], paths[1] = createLocalSnap(c, "pc", "pcididididididididididididididid", 3, "gadget", "", nil)
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{
			"kernel": "other-kernel=18",
			"gadget": "other-gadget=18"},
		sis, paths, "")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchLocalKernelAndGadgetFails(c *C) {
	// Fails as if we use local files, all need to be provided to the API.
	newTrack := map[string]string{"other-kernel": "18", "other-gadget": "18"}
	sis := make([]*snap.SideInfo, 1)
	paths := make([]string, 1)
	sis[0], paths[0] = createLocalSnap(c, "pc-kernel", "pckernelidididididididididididid", 3, "kernel", "", nil)
	s.testRemodelSwitchTasks(c, newTrack,
		map[string]interface{}{
			"kernel": "other-kernel=18",
			"gadget": "other-gadget=18"},
		sis, paths,
		`no snap file provided for "other-gadget"`)
}

func (s *deviceMgrRemodelSuite) testRemodelSwitchTasks(c *C, whatNewTrack map[string]string, newModelOverrides map[string]interface{}, localSnaps []*snap.SideInfo, paths []string, expectedErr string) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testDeviceCtx snapstate.DeviceContext

	var snapstateInstallWithDeviceContextCalled int
	restore := devicestate.MockSnapstateInstallPathWithDeviceContext(func(st *state.State, si *snap.SideInfo, path, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		snapstateInstallWithDeviceContextCalled++
		newTrack, ok := whatNewTrack[name]
		c.Check(ok, Equals, true)
		c.Check(opts.Channel, Equals, newTrack)
		if localSnaps != nil {
			found := false
			for i := range localSnaps {
				if si.RealName == localSnaps[i].RealName {
					found = true
				}
			}
			c.Check(found, Equals, true)
		} else {
			c.Check(si, IsNil)
		}

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()
	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		snapstateInstallWithDeviceContextCalled++
		newTrack, ok := whatNewTrack[name]
		c.Check(ok, Equals, true)
		c.Check(opts.Channel, Equals, newTrack)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	current.KernelSnap().SnapID = "pckernelidididididididididididid"
	current.GadgetSnap().SnapID = "pcididididididididididididididid"
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
		"revision":     "1",
	}
	for k, v := range newModelOverrides {
		headers[k] = v
	}
	new := s.brands.Model("canonical", "pc-model", headers)
	new.KernelSnap().SnapID = "pckernelidididididididididididid"
	new.GadgetSnap().SnapID = "pcididididididididididididididid"

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true, DeviceModel: new, OldDeviceModel: current}

	offline := len(localSnaps) > 0

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", localSnaps, paths, devicestate.RemodelOptions{
		Offline: offline,
	})
	if expectedErr == "" {
		c.Assert(err, IsNil)
		// 1 per switch-kernel/base/gadget plus the remodel task
		c.Assert(tss, HasLen, len(whatNewTrack)+1)
		// API was hit
		c.Assert(snapstateInstallWithDeviceContextCalled, Equals, len(whatNewTrack))
	} else {
		c.Assert(err.Error(), Equals, expectedErr)
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelRequiredSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 2 snaps,
	c.Assert(tl, HasLen, 2*3+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tDownloadSnap1 := tl[0]
	tValidateSnap1 := tl[1]
	tInstallSnap1 := tl[2]
	tDownloadSnap2 := tl[3]
	tValidateSnap2 := tl[4]
	tInstallSnap2 := tl[5]
	tSetModel := tl[6]

	// check the tasks
	c.Assert(tDownloadSnap1.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap1.Summary(), Equals, "Download new-required-snap-1")
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tValidateSnap1.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap1.Summary(), Equals, "Validate new-required-snap-1")
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tDownloadSnap2.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap2.Summary(), Equals, "Download new-required-snap-2")
	// check the ordering, download/validate everything first, then install

	// snap2 downloads wait for the downloads of snap1
	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tValidateSnap1.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap1,
	})
	c.Assert(tDownloadSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap1,
	})
	c.Assert(tValidateSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap2,
	})
	c.Assert(tInstallSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// wait for own check-snap
		tValidateSnap1,
		// and also the last check-snap of the download chain
		tValidateSnap2,
	})
	c.Assert(tInstallSnap2.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain
		tValidateSnap2,
		// previous install chain
		tInstallSnap1,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{tDownloadSnap1, tValidateSnap1, tInstallSnap1, tDownloadSnap2, tValidateSnap2, tInstallSnap2})
}

func (s *deviceMgrRemodelSuite) TestRemodelSwitchKernelTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 2*3+1)

	tDownloadKernel := tl[0]
	tValidateKernel := tl[1]
	tUpdateKernel := tl[2]
	tDownloadSnap1 := tl[3]
	tValidateSnap1 := tl[4]
	tInstallSnap1 := tl[5]
	tSetModel := tl[6]

	c.Assert(tDownloadKernel.Kind(), Equals, "fake-download")
	c.Assert(tDownloadKernel.Summary(), Equals, "Download pc-kernel from track 18")
	c.Assert(tValidateKernel.Kind(), Equals, "validate-snap")
	c.Assert(tValidateKernel.Summary(), Equals, "Validate pc-kernel")
	c.Assert(tUpdateKernel.Kind(), Equals, "fake-update")
	c.Assert(tUpdateKernel.Summary(), Equals, "Update pc-kernel to track 18")
	c.Assert(tDownloadSnap1.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap1.Summary(), Equals, "Download new-required-snap-1")
	c.Assert(tValidateSnap1.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap1.Summary(), Equals, "Validate new-required-snap-1")
	c.Assert(tInstallSnap1.Kind(), Equals, "fake-install")
	c.Assert(tInstallSnap1.Summary(), Equals, "Install new-required-snap-1")

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")

	// check the ordering
	c.Assert(tDownloadSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// previous download finished
		tValidateKernel,
	})
	c.Assert(tInstallSnap1.WaitTasks(), DeepEquals, []*state.Task{
		// last download in the chain finished
		tValidateSnap1,
		// and kernel got updated
		tUpdateKernel,
	})
	c.Assert(tUpdateKernel.WaitTasks(), DeepEquals, []*state.Task{
		// kernel is valid
		tValidateKernel,
		// and last download in the chain finished
		tValidateSnap1,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelLessRequiredSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"some-required-snap"},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
		"revision":     "1",
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 1)
	tSetModel := tl[0]
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
}

type freshSessionStore struct {
	storetest.Store

	ensureDeviceSession int
}

func (sto *freshSessionStore) EnsureDeviceSession() error {
	sto.ensureDeviceSession += 1
	return nil
}

func (s *deviceMgrRemodelSuite) TestRemodelStoreSwitch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testStore snapstate.StoreService

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		c.Check(deviceCtx.Store(), Equals, testStore)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"store":          "switched-store",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})

	freshStore := &freshSessionStore{}
	testStore = freshStore

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return testStore
	}

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	c.Check(freshStore.ensureDeviceSession, Equals, 1)

	tl := chg.Tasks()
	// 2 snaps * 3 tasks (from the mock install above) +
	// 1 "set-model" task at the end
	c.Assert(tl, HasLen, 2*3+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.StoreSwitchRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), Equals, testStore)
}

func (s *deviceMgrRemodelSuite) TestRemodelRereg(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		Serial:          "orig-serial",
		SessionMacaroon: "old-session",
	})

	new := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	c.Assert(chg.Summary(), Equals, "Remodel device to canonical/rereg-model (0)")

	tl := chg.Tasks()
	c.Assert(tl, HasLen, 2)

	// check the tasks
	tRequestSerial := tl[0]
	tPrepareRemodeling := tl[1]

	// check the tasks
	c.Assert(tRequestSerial.Kind(), Equals, "request-serial")
	c.Assert(tRequestSerial.Summary(), Equals, "Request new device serial")
	c.Assert(tRequestSerial.WaitTasks(), HasLen, 0)

	c.Assert(tPrepareRemodeling.Kind(), Equals, "prepare-remodeling")
	c.Assert(tPrepareRemodeling.Summary(), Equals, "Prepare remodeling")
	c.Assert(tPrepareRemodeling.WaitTasks(), DeepEquals, []*state.Task{tRequestSerial})
}

func (s *deviceMgrRemodelSuite) TestRemodelReregLocalFails(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		Serial:          "orig-serial",
		SessionMacaroon: "old-session",
	})

	new := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod, err := devBE.Model()
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return nil
	}

	sis := []*snap.SideInfo{{RealName: "pc-kernel"}, {RealName: "pc"}}
	paths := []string{"pc-kernel_1.snap", "pc_1.snap"}
	chg, err := devicestate.Remodel(s.state, new, sis, paths, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err.Error(), Equals, "cannot remodel offline to different brand ID / model yet")
	c.Assert(chg, IsNil)
}

func (s *deviceMgrRemodelSuite) TestRemodelClash(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var clashing *asserts.Model

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate things changing under our feet
		assertstatetest.AddMany(st, clashing)
		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand: "canonical",
			Model: clashing.Model(),
		})

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	other := s.brands.Model("canonical", "pc-model-other", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	clashing = other
	_, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model-other (0)",
	})

	// reset
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})
	clashing = new
	_, err = devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model (1)",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelClashInProgress(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var chg *state.Change
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate another started remodeling
		chg = st.NewChange("remodel", "other remodel")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1"},
		"revision":       "1",
	})

	_, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message:    "cannot start remodel, clashing with concurrent one",
		ChangeKind: "remodel",
		ChangeID:   chg.ID(),
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelClashWithRecoverySystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var chg *state.Change
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate another recovery system being created
		chg = s.state.NewChange("create-recovery-system", "...")
		chg.AddTask(s.state.NewTask("fake-create-recovery-system", "..."))

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "1234")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "1234",
	})

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1"},
		"revision":       "1",
	})

	_, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message:    "creating recovery system in progress, no other changes allowed until this is done",
		ChangeKind: chg.Kind(),
		ChangeID:   chg.ID(),
	})
}

func (s *deviceMgrRemodelSuite) TestReregRemodelClashAnyChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		Serial:          "orig-serial",
		SessionMacaroon: "old-session",
	})

	new := s.brands.Model("canonical", "pc-model-2", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		// we never reach the place where this gets called
		c.Fatalf("unexpected call")
		return nil
	}

	// simulate any other change
	chg := s.state.NewChange("chg", "other change")
	chg.SetStatus(state.DoingStatus)

	_, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, NotNil)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "chg",
		Message:    `other changes in progress (conflicting change "chg"), change "remodel" not allowed until they are done`,
		ChangeID:   chg.ID(),
	})
}

func (s *deviceMgrRemodelSuite) TestRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no changes
	c.Check(devicestate.RemodelingChange(s.state), IsNil)

	// other change
	s.state.NewChange("other", "...")
	c.Check(devicestate.RemodelingChange(s.state), IsNil)

	// remodel change
	chg := s.state.NewChange("remodel", "...")
	c.Check(devicestate.RemodelingChange(s.state), NotNil)

	// done
	chg.SetStatus(state.DoneStatus)
	c.Check(devicestate.RemodelingChange(s.state), IsNil)
}

func (s *deviceMgrRemodelSuite) TestDeviceCtxNoTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing in the state

	_, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// have a model assertion
	model := s.brands.Model("canonical", "pc", map[string]interface{}{
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	})
	assertstatetest.AddMany(s.state, model)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	deviceCtx, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx.Model().BrandID(), Equals, "canonical")

	c.Check(deviceCtx.Classic(), Equals, false)
	c.Check(deviceCtx.Kernel(), Equals, "kernel")
	c.Check(deviceCtx.Base(), Equals, "")
	c.Check(deviceCtx.RunMode(), Equals, true)
	// not a uc20 model, so no modeenv
	c.Check(deviceCtx.HasModeenv(), Equals, false)
}

func (s *deviceMgrRemodelSuite) TestDeviceCtxGroundContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model assertion
	model := s.brands.Model("canonical", "pc", map[string]interface{}{
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	})
	assertstatetest.AddMany(s.state, model)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	deviceCtx, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx.Model().BrandID(), Equals, "canonical")
	groundCtx := deviceCtx.GroundContext()
	c.Check(groundCtx.ForRemodeling(), Equals, false)
	c.Check(groundCtx.Model().Model(), Equals, "pc")
	c.Check(groundCtx.Store, PanicMatches, `retrieved ground context is not intended to drive store operations`)
}

func (s *deviceMgrRemodelSuite) TestDeviceCtxProvided(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	model := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	}).(*asserts.Model)

	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	deviceCtx1, err := devicestate.DeviceCtx(s.state, nil, deviceCtx)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx1, Equals, deviceCtx)
}

func (s *deviceMgrRemodelSuite) TestCheckGadgetRemodelCompatible(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	currentSnapYaml := `
name: gadget
type: gadget
version: 123
`
	remodelSnapYaml := `
name: new-gadget
type: gadget
version: 123
`
	mockGadget := `
type: gadget
name: gadget
volumes:
  volume:
    schema: gpt
    bootloader: grub
`
	siCurrent := &snap.SideInfo{Revision: snap.R(123), RealName: "gadget"}
	// so that we get a directory
	currInfo := snaptest.MockSnapWithFiles(c, currentSnapYaml, siCurrent, nil)
	info := snaptest.MockSnapWithFiles(c, remodelSnapYaml, &snap.SideInfo{Revision: snap.R(1)}, nil)
	snapf, err := snapfile.Open(info.MountDir())
	c.Assert(err, IsNil)

	s.setupBrands()

	oldModel := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "kernel",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: oldModel}

	// model assertion in device context
	newModel := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "new-gadget",
		"kernel":       "kernel",
	})
	remodelCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: newModel, Remodeling: true, OldDeviceModel: oldModel}

	restore := devicestate.MockGadgetIsCompatible(func(current, update *gadget.Info) error {
		c.Assert(current.Volumes, HasLen, 1)
		c.Assert(update.Volumes, HasLen, 1)
		return errors.New("fail")
	})
	defer restore()

	// not on classic
	release.OnClassic = true
	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, IsNil)
	release.OnClassic = false

	// nothing if not remodeling
	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot read new gadget metadata: .*/new-gadget/1/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml to the new gadget
	err = os.WriteFile(filepath.Join(info.MountDir(), "meta/gadget.yaml"), []byte(mockGadget), 0644)
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot read current gadget metadata: .*/gadget/123/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml to the current gadget
	err = os.WriteFile(filepath.Join(currInfo.MountDir(), "meta/gadget.yaml"), []byte(mockGadget), 0644)
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot remodel to an incompatible gadget: fail")

	restore = devicestate.MockGadgetIsCompatible(func(current, update *gadget.Info) error {
		c.Assert(current.Volumes, HasLen, 1)
		c.Assert(update.Volumes, HasLen, 1)
		return nil
	})
	defer restore()

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, IsNil)

	// when remodeling to completely new gadget snap, there is no current
	// snap passed to the check callback
	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, nil, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot identify the current gadget snap")

	// mock data to obtain current gadget info
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "gadget",
	})
	s.makeModelAssertionInState(c, "canonical", "gadget", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "kernel",
		"gadget":       "gadget",
	})

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, nil, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot identify the current gadget snap")

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, nil, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, IsNil)
}

var (
	compatibleTestMockOkGadget = `
type: gadget
name: gadget
volumes:
  volume:
    schema: gpt
    bootloader: grub
    structure:
      - name: foo
        size: 10M
        type: 00000000-0000-0000-0000-0000deadbeef
`
)

func (s *deviceMgrRemodelSuite) testCheckGadgetRemodelCompatibleWithYaml(c *C, currentGadgetYaml, newGadgetYaml string, expErr string) {
	s.state.Lock()
	defer s.state.Unlock()

	currentSnapYaml := `
name: gadget
type: gadget
version: 123
`
	remodelSnapYaml := `
name: new-gadget
type: gadget
version: 123
`

	currInfo := snaptest.MockSnapWithFiles(c, currentSnapYaml, &snap.SideInfo{Revision: snap.R(123)}, [][]string{
		{"meta/gadget.yaml", currentGadgetYaml},
	})
	// gadget we're remodeling to is identical
	info := snaptest.MockSnapWithFiles(c, remodelSnapYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
	})
	snapf, err := snapfile.Open(info.MountDir())
	c.Assert(err, IsNil)

	s.setupBrands()
	// model assertion in device context
	oldModel := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "new-gadget",
		"kernel":       "krnl",
	})
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "new-gadget",
		"kernel":       "krnl",
	})
	remodelCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model, Remodeling: true, OldDeviceModel: oldModel}

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	if expErr == "" {
		c.Check(err, IsNil)
	} else {
		c.Check(err, ErrorMatches, expErr)
	}

}

func (s *deviceMgrRemodelSuite) TestCheckGadgetRemodelCompatibleWithYamlHappy(c *C) {
	s.testCheckGadgetRemodelCompatibleWithYaml(c, compatibleTestMockOkGadget, compatibleTestMockOkGadget, "")
}

func (s *deviceMgrRemodelSuite) TestCheckGadgetRemodelCompatibleWithYamlBad(c *C) {
	mockBadGadgetYaml := `
type: gadget
name: gadget
volumes:
  volume:
    schema: gpt
    bootloader: grub
    structure:
      - name: foo
        size: 20M
        type: 00000000-0000-0000-0000-0000deadbeef
`

	errMatch := `cannot remodel to an incompatible gadget: incompatible layout change: incompatible structure #0 \("foo"\) change: new valid structure size range \[20971520, 20971520\] is not compatible with current \(\[10485760, 10485760\]\)`
	s.testCheckGadgetRemodelCompatibleWithYaml(c, compatibleTestMockOkGadget, mockBadGadgetYaml, errMatch)
}

func (s *deviceMgrRemodelSuite) mockTasksNopHandler(kinds ...string) {
	nopHandler := func(task *state.Task, _ *tomb.Tomb) error { return nil }
	for _, kind := range kinds {
		s.o.TaskRunner().AddHandler(kind, nopHandler, nil)
	}
}

func asOffsetPtr(offs quantity.Offset) *quantity.Offset {
	goff := offs
	return &goff
}

func (s *deviceMgrRemodelSuite) TestRemodelGadgetAssetsUpdate(c *C) {
	var currentGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         type: 00000000-0000-0000-0000-0000deadcafe
         filesystem: ext4
         size: 10M
         content:
            - source: foo-content
              target: /
       - name: bare-one
         type: bare
         size: 1M
         content:
            - image: bare.img
`

	var remodelGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         type: 00000000-0000-0000-0000-0000deadcafe
         filesystem: ext4
         size: 10M
         content:
            - source: new-foo-content
              target: /
       - name: bare-one
         type: bare
         size: 1M
         content:
            - image: new-bare-content.img
`

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	s.mockTasksNopHandler("fake-download", "validate-snap", "set-model")

	// set a model assertion we remodel from
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	kernelInfo := snapstatetest.InstallSnap(c, s.state, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: core18\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core18"),
		Revision: snap.R(1),
		RealName: "core18",
	}, snapstatetest.InstallSnapOptions{Required: true})

	devicestate.SetBootRevisionsUpdated(s.mgr, true)

	// the target model
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"base":         "core18",
		"revision":     "1",
		// remodel to new gadget
		"gadget": "new-gadget",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	currentGadgetInfo := snaptest.MockSnapWithFiles(c, snapYaml, siModelGadget, [][]string{
		{"meta/gadget.yaml", currentGadgetYaml},
	})
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:  siModelGadget.Revision,
		Active:   true,
	})

	// new gadget snap
	siNewModelGadget := &snap.SideInfo{
		RealName: "new-gadget",
		Revision: snap.R(34),
	}
	newGadgetInfo := snaptest.MockSnapWithFiles(c, snapYaml, siNewModelGadget, [][]string{
		{"meta/gadget.yaml", remodelGadgetYaml},
	})

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tGadgetUpdate := s.state.NewTask("update-gadget-assets", fmt.Sprintf("Update gadget %s", name))
		tGadgetUpdate.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: siNewModelGadget,
			Type:     snap.TypeGadget,
		})
		tGadgetUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tGadgetUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	gadgetUpdateCalled := false
	restore = devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		gadgetUpdateCalled = true
		c.Check(policy, NotNil)
		c.Check(reflect.ValueOf(policy).Pointer(), Equals, reflect.ValueOf(gadget.RemodelUpdatePolicy).Pointer())
		gd := gadget.GadgetData{
			Info: &gadget.Info{
				Volumes: map[string]*gadget.Volume{
					"pc": {
						Name:       "pc",
						Bootloader: "grub",
						Schema:     "gpt",
						Structure: []gadget.VolumeStructure{{
							VolumeName: "pc",
							Name:       "foo",
							Type:       "00000000-0000-0000-0000-0000deadcafe",
							Offset:     asOffsetPtr(gadget.NonMBRStartOffset),
							MinSize:    10 * quantity.SizeMiB,
							Size:       10 * quantity.SizeMiB,
							Filesystem: "ext4",
							Content: []gadget.VolumeContent{
								{UnresolvedSource: "foo-content", Target: "/"},
							},
							YamlIndex:       0,
							EnclosingVolume: &gadget.Volume{},
						}, {
							VolumeName: "pc",
							Name:       "bare-one",
							Type:       "bare",
							Offset:     asOffsetPtr(gadget.NonMBRStartOffset + 10*quantity.OffsetMiB),
							MinSize:    quantity.SizeMiB,
							Size:       quantity.SizeMiB,
							Content: []gadget.VolumeContent{
								{Image: "bare.img"},
							},
							YamlIndex:       1,
							EnclosingVolume: &gadget.Volume{},
						}},
					},
				},
			},
			RootDir:       currentGadgetInfo.MountDir(),
			KernelRootDir: kernelInfo.MountDir(),
		}
		gadget.SetEnclosingVolumeInStructs(gd.Info.Volumes)
		c.Check(current, DeepEquals, gd)
		gd = gadget.GadgetData{
			Info: &gadget.Info{
				Volumes: map[string]*gadget.Volume{
					"pc": {
						Name:       "pc",
						Bootloader: "grub",
						Schema:     "gpt",
						Structure: []gadget.VolumeStructure{{
							VolumeName: "pc",
							Name:       "foo",
							Type:       "00000000-0000-0000-0000-0000deadcafe",
							Offset:     asOffsetPtr(gadget.NonMBRStartOffset),
							MinSize:    10 * quantity.SizeMiB,
							Size:       10 * quantity.SizeMiB,
							Filesystem: "ext4",
							Content: []gadget.VolumeContent{
								{UnresolvedSource: "new-foo-content", Target: "/"},
							},
							YamlIndex: 0,
						}, {
							VolumeName: "pc",
							Name:       "bare-one",
							Type:       "bare",
							Offset:     asOffsetPtr(gadget.NonMBRStartOffset + 10*quantity.OffsetMiB),
							MinSize:    quantity.SizeMiB,
							Size:       quantity.SizeMiB,
							Content: []gadget.VolumeContent{
								{Image: "new-bare-content.img"},
							},
							YamlIndex: 1,
						}},
					},
				},
			},
			RootDir:       newGadgetInfo.MountDir(),
			KernelRootDir: kernelInfo.MountDir(),
		}
		gadget.SetEnclosingVolumeInStructs(gd.Info.Volumes)
		c.Check(update, DeepEquals, gd)
		return nil
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// simulate restart
	s.mockRestartAndSettle(c, s.state, chg)

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(gadgetUpdateCalled, Equals, true)
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystem})
}

func (s *deviceMgrRemodelSuite) TestRemodelGadgetAssetsParanoidCheck(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	s.mockTasksNopHandler("fake-download", "validate-snap", "set-model")

	// set a model assertion we remodel from
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	snapstatetest.InstallSnap(c, s.state, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: core18\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core18"),
		Revision: snap.R(1),
		RealName: "core18",
	}, snapstatetest.InstallSnapOptions{Required: true})

	devicestate.SetBootRevisionsUpdated(s.mgr, true)

	// the target model
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"base":         "core18",
		"revision":     "1",
		// remodel to new gadget
		"gadget": "new-gadget",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:  siModelGadget.Revision,
		Active:   true,
	})

	// new gadget snap, name does not match the new model
	siUnexpectedModelGadget := &snap.SideInfo{
		RealName: "new-gadget-unexpected",
		Revision: snap.R(34),
	}
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tGadgetUpdate := s.state.NewTask("update-gadget-assets", fmt.Sprintf("Update gadget %s", name))
		tGadgetUpdate.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: siUnexpectedModelGadget,
			Type:     snap.TypeGadget,
		})
		tGadgetUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tGadgetUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	gadgetUpdateCalled := false
	restore = devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), ErrorMatches, `(?s).*\(cannot apply gadget assets update from non-model gadget snap "new-gadget-unexpected", expected "new-gadget" snap\)`)
	c.Check(gadgetUpdateCalled, Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestRemodelSwitchBaseIncompatibleGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testDeviceCtx snapstate.DeviceContext

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(name, Equals, "core20")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core20",
		"revision":     "1",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true, DeviceModel: new, OldDeviceModel: current}

	_, err = devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, `cannot remodel with gadget snap that has a different base than the model: "core18" \!= "core20"`)
}

func (s *deviceMgrSuite) TestRemodelSwitchBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testDeviceCtx snapstate.DeviceContext

	var snapstateInstallWithDeviceContextCalled int
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		snapstateInstallWithDeviceContextCalled++
		switch name {
		case "core20", "pc-20":
		default:
			c.Errorf("unexpected snap name %q", name)
		}

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc-20",
		"base":         "core20",
		"revision":     "1",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true, DeviceModel: new, OldDeviceModel: current}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	// 1 switch to a new base, 1 switch to new gadget, plus the remodel task
	c.Assert(tss, HasLen, 3)
	// API was hit
	c.Assert(snapstateInstallWithDeviceContextCalled, Equals, 2)
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20RequiredSnapsAndRecoverySystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "snapd",
				"id":              snaptest.AssertedSnapID("snapd"),
				"type":            "snapd",
				"default-channel": "latest",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	// and snapd
	siModelSnapd := &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(55),
		SnapID:   snaptest.AssertedSnapID("snapd"),
	}
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		SnapType:        "snapd",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelSnapd}),
		Current:         siModelSnapd.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	// New model, that changes snapd tracking channel and with 2 new required snaps
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "snapd",
				"id":              snaptest.AssertedSnapID("snapd"),
				"type":            "snapd",
				"default-channel": "latest/edge",
			},
			map[string]interface{}{
				"name":     "new-required-snap-1",
				"id":       snaptest.AssertedSnapID("new-required-snap-1"),
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "new-required-snap-2",
				"id":       snaptest.AssertedSnapID("new-required-snap-2"),
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "new-optional-snap-1",
				"id":       snaptest.AssertedSnapID("new-optional-snap-1"),
				"presence": "optional",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 3 snaps (3 tasks for each) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 3*3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tDownloadSnap1 := tl[0]
	tValidateSnap1 := tl[1]
	tInstallSnap1 := tl[2]
	tDownloadSnap2 := tl[3]
	tValidateSnap2 := tl[4]
	tInstallSnap2 := tl[5]
	tDownloadSnap3 := tl[6]
	tValidateSnap3 := tl[7]
	tInstallSnap3 := tl[8]
	tCreateRecovery := tl[9]
	tFinalizeRecovery := tl[10]
	tSetModel := tl[11]

	// check the tasks

	c.Assert(tDownloadSnap1.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap1.Summary(), Equals, "Download snapd from track latest/edge")
	c.Assert(tValidateSnap1.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap1.Summary(), Equals, "Validate snapd")

	c.Assert(tDownloadSnap2.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap2.Summary(), Equals, "Download new-required-snap-1")
	c.Assert(tValidateSnap2.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap2.Summary(), Equals, "Validate new-required-snap-1")

	c.Assert(tDownloadSnap3.Kind(), Equals, "fake-download")
	c.Assert(tDownloadSnap3.Summary(), Equals, "Download new-required-snap-2")
	c.Assert(tValidateSnap3.Kind(), Equals, "validate-snap")
	c.Assert(tValidateSnap3.Summary(), Equals, "Validate new-required-snap-2")

	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))

	// check the ordering, download/validate everything first, then install

	c.Assert(tDownloadSnap1.WaitTasks(), HasLen, 0)
	c.Assert(tValidateSnap1.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap1,
	})
	c.Assert(tInstallSnap1.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap1,
		tValidateSnap3,
		// wait for recovery system to be created
		tCreateRecovery,
		// and then finalized
		tFinalizeRecovery,
	})

	// snap2 downloads wait for the downloads of snap1
	c.Assert(tDownloadSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap1,
	})
	c.Assert(tValidateSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap2,
	})
	c.Assert(tInstallSnap2.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap2,
		tInstallSnap1,
	})
	c.Assert(tInstallSnap3.WaitTasks(), DeepEquals, []*state.Task{
		tValidateSnap3,
		// previous install chain
		tInstallSnap2,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain
		tValidateSnap3,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (added later)
		tValidateSnap3,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadSnap1, tValidateSnap1, tInstallSnap1,
		tDownloadSnap2, tValidateSnap2, tInstallSnap2,
		tDownloadSnap3, tValidateSnap3, tInstallSnap3,
		tCreateRecovery, tFinalizeRecovery,
	})

	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tDownloadSnap1.ID(), tDownloadSnap2.ID(), tDownloadSnap3.ID()},
		"test-system":      true,
	})
	// cross references of to recovery system setup data
	for _, tsk := range []*state.Task{tFinalizeRecovery, tSetModel} {
		var otherTaskID string
		// finalize-recovery-system points to create-recovery-system
		err = tsk.Get("recovery-system-setup-task", &otherTaskID)
		c.Assert(err, IsNil, Commentf("recovery system setup task ID missing in %s", tsk.Kind()))
		c.Assert(otherTaskID, Equals, tCreateRecovery.ID())
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelGadgetBaseSnaps(c *C) {
	s.testRemodelUC20SwitchKernelGadgetBaseSnaps(c, &prepareRemodelFlags{})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelGadgetBaseSnapsLocalSnaps(c *C) {
	s.testRemodelUC20SwitchKernelGadgetBaseSnaps(c, &prepareRemodelFlags{localSnaps: true})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelGadgetBaseSnapsLocalSnapsFails(c *C) {
	s.testRemodelUC20SwitchKernelGadgetBaseSnaps(c,
		&prepareRemodelFlags{localSnaps: true, missingSnap: true})
}

type prepareRemodelFlags struct {
	localSnaps  bool
	missingSnap bool
}

func (s *deviceMgrRemodelSuite) testRemodelUC20SwitchKernelGadgetBaseSnaps(c *C, testFlags *prepareRemodelFlags) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(testFlags.localSnaps, Equals, false)

		// This task would not really be added if we have a local snap,
		// but we keep it anyway to simplify the checks we do at the end.
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdatePathWithDeviceContext(func(st *state.State, si *snap.SideInfo, path, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(si, NotNil)
		c.Check(si.RealName, Equals, name)

		// This task would not really be added if we have a local snap,
		// but we keep it anyway to simplify the checks we do at the end.
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// snaps will be refreshed so calls go through update
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:  siModelGadget.Revision,
		Active:   true,
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:  siModelKernel.Revision,
		Active:   true,
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	// new gadget
	newGadget := "pc"
	if testFlags.missingSnap {
		newGadget = "pc-new"
	}

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "21/edge",
			},
			map[string]interface{}{
				"name":            newGadget,
				"id":              snaptest.AssertedSnapID(newGadget),
				"type":            "gadget",
				"default-channel": "21/stable",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "latest/edge",
			},
		},
	})

	var localSnaps []*snap.SideInfo
	var paths []string
	if testFlags.localSnaps {
		localSnaps = []*snap.SideInfo{siModelKernel, siModelBase}
		paths = []string{"pc-kernel_101.snap", "core20"}
		if !testFlags.missingSnap {
			localSnaps = append(localSnaps, siModelGadget)
			paths = append(paths, "pc_101.snap")
		}
	}

	chg, err := devicestate.Remodel(s.state, new, localSnaps, paths, devicestate.RemodelOptions{
		Offline: testFlags.localSnaps,
	})
	if testFlags.missingSnap {
		c.Assert(chg, IsNil)
		c.Assert(err, ErrorMatches, `no snap file provided for "pc-new"`)
		return
	}

	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 3 snaps (3 tasks for each) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 3*3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tDownloadKernel := tl[0]
	tValidateKernel := tl[1]
	tInstallKernel := tl[2]
	tDownloadBase := tl[3]
	tValidateBase := tl[4]
	tInstallBase := tl[5]
	tDownloadGadget := tl[6]
	tValidateGadget := tl[7]
	tInstallGadget := tl[8]
	tCreateRecovery := tl[9]
	tFinalizeRecovery := tl[10]
	tSetModel := tl[11]

	// check the tasks
	c.Assert(tDownloadKernel.Kind(), Equals, "fake-download")
	c.Assert(tDownloadKernel.Summary(), Equals, "Download pc-kernel from track 21/edge")
	c.Assert(tDownloadKernel.WaitTasks(), HasLen, 0)
	c.Assert(tValidateKernel.Kind(), Equals, "validate-snap")
	c.Assert(tValidateKernel.Summary(), Equals, "Validate pc-kernel")
	c.Assert(tDownloadBase.Kind(), Equals, "fake-download")
	c.Assert(tDownloadBase.Summary(), Equals, "Download core20 from track latest/edge")
	c.Assert(tDownloadBase.WaitTasks(), HasLen, 1)
	c.Assert(tValidateBase.Kind(), Equals, "validate-snap")
	c.Assert(tValidateBase.Summary(), Equals, "Validate core20")
	c.Assert(tDownloadGadget.Kind(), Equals, "fake-download")
	c.Assert(tDownloadGadget.Summary(), Equals, "Download pc from track 21/stable")
	c.Assert(tDownloadGadget.WaitTasks(), HasLen, 1)
	c.Assert(tValidateGadget.Kind(), Equals, "validate-snap")
	c.Assert(tValidateGadget.Summary(), Equals, "Validate pc")
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))

	// check the ordering, download/validate everything first, then install
	// gadget downloads wait for the downloads of kernel
	c.Assert(tDownloadKernel.WaitTasks(), HasLen, 0)
	c.Assert(tValidateKernel.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadKernel,
	})
	c.Assert(tInstallKernel.WaitTasks(), DeepEquals, []*state.Task{
		tValidateKernel,
		tValidateGadget,
		// wait for recovery system to be created
		tCreateRecovery,
		// and then finalized
		tFinalizeRecovery,
	})
	c.Assert(tInstallBase.WaitTasks(), DeepEquals, []*state.Task{
		tValidateBase,
		// previous install chain
		tInstallKernel,
	})
	c.Assert(tInstallGadget.WaitTasks(), DeepEquals, []*state.Task{
		tValidateGadget,
		// previous install chain
		tInstallBase,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain
		tValidateGadget,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (added later)
		tValidateGadget,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadKernel, tValidateKernel, tInstallKernel,
		tDownloadBase, tValidateBase, tInstallBase,
		tDownloadGadget, tValidateGadget, tInstallGadget,
		tCreateRecovery, tFinalizeRecovery,
	})

	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tDownloadKernel.ID(), tDownloadBase.ID(), tDownloadGadget.ID()},
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelOfflineUseInstalledSnaps(c *C) {
	// remodel switches to a new set of kernel, base and gadget snaps, but some
	// of those (kernel, base) happen to be already installed and tracking the
	// right channels.
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallPathWithDeviceContext(func(_ *state.State, si *snap.SideInfo, _ string, name string, opts *snapstate.RevisionOptions, _ int, _ snapstate.Flags, _ snapstate.PrereqTracker, _ snapstate.DeviceContext, _ string) (*state.TaskSet, error) {
		c.Check(si.RealName, Equals, "app-snap")

		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.Set("snap-setup",
			&snapstate.SnapSetup{SideInfo: si, Channel: opts.Channel})
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core24",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20/stable",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// install snaps for current model
	snapstatetest.InstallEssentialSnaps(c, s.state, "core24", nil, nil)

	// install snaps that will be needed for new model
	snapstatetest.InstallSnap(c, s.state, "name: pc-new\nversion: 1\ntype: gadget\nbase: core24-new", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-new"),
		Revision: snap.R(222),
		RealName: "pc-new",
		Channel:  "20/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: pc-kernel-new\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel-new"),
		Revision: snap.R(222),
		RealName: "pc-kernel-new",
		Channel:  "20/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: core24-new\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core24-new"),
		Revision: snap.R(222),
		RealName: "core24-new",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	// not yet installed app-snap, that will be provided as a local snap
	appSnap := &snap.SideInfo{
		RealName: "app-snap",
		Revision: snap.R(222),
		SnapID:   snaptest.AssertedSnapID("app-snap"),
		Channel:  "latest/stable",
	}
	appSnapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: app-snap\nversion: 1\ntype: app\n", nil, appSnap)

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		// switch to a new base which is already installed
		"base":     "core24-new",
		"grade":    "dangerous",
		"revision": "1",
		"snaps": []interface{}{
			map[string]interface{}{
				// switch to a new kernel which also is already
				// installed
				"name":            "pc-kernel-new",
				"id":              snaptest.AssertedSnapID("pc-kernel-new"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc-new",
				"id":              snaptest.AssertedSnapID("pc-new"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "app-snap",
				"id":              snaptest.AssertedSnapID("app-snap"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})

	chg, err := devicestate.Remodel(s.state, new, []*snap.SideInfo{appSnap}, []string{appSnapPath}, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()

	for _, t := range tl {
		c.Logf("%s: %s", t.Kind(), t.Summary())
	}

	// 3 snaps (2 tasks for each) + assets update and setup from kernel + gadget (3 tasks) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 3*2+2+3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tPrepareKernel := tl[0]
	tSetupKernelSnap := tl[1]
	tUpdateAssetsKernel := tl[2]
	tLinkKernel := tl[3]
	tPrepareBase := tl[4]
	tLinkBase := tl[5]
	tPrepareGadget := tl[6]
	tUpdateAssets := tl[7]
	tUpdateCmdline := tl[8]
	tValidateApp := tl[9]
	tInstallApp := tl[10]
	tCreateRecovery := tl[11]
	tFinalizeRecovery := tl[12]
	tSetModel := tl[13]

	// check the tasks
	c.Assert(tPrepareKernel.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareKernel.Summary(), Equals, `Prepare snap "pc-kernel-new" (222) for remodel`)
	c.Assert(tPrepareKernel.WaitTasks(), HasLen, 0)
	c.Assert(tSetupKernelSnap.Kind(), Equals, "setup-kernel-snap")
	c.Assert(tSetupKernelSnap.Summary(), Equals, `Setup kernel driver tree for "pc-kernel-new" (222) for remodel`)
	c.Assert(tLinkKernel.Kind(), Equals, "link-snap")
	c.Assert(tLinkKernel.Summary(), Equals, `Make snap "pc-kernel-new" (222) available to the system during remodel`)
	c.Assert(tUpdateAssetsKernel.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssetsKernel.Summary(), Equals, `Update assets from kernel "pc-kernel-new" (222) for remodel`)
	c.Assert(tPrepareBase.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareBase.Summary(), Equals, `Prepare snap "core24-new" (222) for remodel`)
	c.Assert(tPrepareBase.WaitTasks(), HasLen, 1)
	c.Assert(tLinkBase.Kind(), Equals, "link-snap")
	c.Assert(tLinkBase.Summary(), Equals, `Make snap "core24-new" (222) available to the system during remodel`)
	c.Assert(tPrepareGadget.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareGadget.Summary(), Equals, `Prepare snap "pc-new" (222) for remodel`)
	c.Assert(tPrepareGadget.WaitTasks(), HasLen, 1)
	c.Assert(tUpdateAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssets.Summary(), Equals, `Update assets from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateAssets.WaitTasks(), HasLen, 2)
	c.Assert(tUpdateCmdline.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateCmdline.Summary(), Equals, `Update kernel command line from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateCmdline.WaitTasks(), HasLen, 1)
	c.Assert(tValidateApp.Kind(), Equals, "validate-snap")
	c.Assert(tValidateApp.Summary(), Equals, "Validate app-snap")
	c.Assert(tInstallApp.Kind(), Equals, "fake-install")
	c.Assert(tInstallApp.Summary(), Equals, "Install app-snap")
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// check the ordering, prepare/link are part of download edge and come first
	c.Assert(tPrepareKernel.WaitTasks(), HasLen, 0)
	c.Assert(tLinkKernel.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssetsKernel,
	})
	c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel,
		tValidateApp,
		tCreateRecovery,
		tFinalizeRecovery,
	})
	c.Assert(tUpdateAssetsKernel.WaitTasks(), DeepEquals, []*state.Task{
		tSetupKernelSnap,
	})
	c.Assert(tPrepareBase.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel,
	})
	c.Assert(tLinkBase.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
		tLinkKernel,
	})
	c.Assert(tPrepareGadget.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
	})
	c.Assert(tUpdateAssets.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareGadget,
		tLinkBase,
	})
	c.Assert(tUpdateCmdline.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssets,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain (in this case, validate the locally
		// provided snap)
		tValidateApp,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (see above)
		tValidateApp,
	})
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel, tSetupKernelSnap, tUpdateAssetsKernel,
		tLinkKernel, tPrepareBase, tLinkBase,
		tPrepareGadget, tUpdateAssets, tUpdateCmdline,
		tValidateApp, tInstallApp,
		tCreateRecovery, tFinalizeRecovery,
	})
	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tPrepareKernel.ID(), tPrepareBase.ID(), tPrepareGadget.ID(), tValidateApp.ID()},
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelOfflineUseInstalledSnapsChannelSwitch(c *C) {
	// remodel switches to a new set of kernel, base and gadget snaps. some of
	// those (kernel, base) happen to be already installed, and the channel must
	// be switched.
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallPathWithDeviceContext(func(_ *state.State, si *snap.SideInfo, _ string, name string, opts *snapstate.RevisionOptions, _ int, _ snapstate.Flags, _ snapstate.PrereqTracker, _ snapstate.DeviceContext, _ string) (*state.TaskSet, error) {
		c.Check(si.RealName, Equals, "app-snap")

		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.Set("snap-setup",
			&snapstate.SnapSetup{SideInfo: si, Channel: opts.Channel})
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core24",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20/stable",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// install snaps for current model
	snapstatetest.InstallEssentialSnaps(c, s.state, "core24", nil, nil)

	// install snaps that will be needed for new model
	snapstatetest.InstallSnap(c, s.state, "name: pc-new\nversion: 1\ntype: gadget\nbase: core24-new", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-new"),
		Revision: snap.R(222),
		RealName: "pc-new",
		Channel:  "20/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: pc-kernel-new\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel-new"),
		Revision: snap.R(222),
		RealName: "pc-kernel-new",
		Channel:  "20/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: core24-new\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core24-new"),
		Revision: snap.R(222),
		RealName: "core24-new",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	// not yet installed app-snap, that will be provided as a local snap
	appSnap := &snap.SideInfo{
		RealName: "app-snap",
		Revision: snap.R(222),
		SnapID:   snaptest.AssertedSnapID("app-snap"),
		Channel:  "latest/stable",
	}
	appSnapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: app-snap\nversion: 1\ntype: app\n", nil, appSnap)

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		// switch to a new base which is already installed
		"base":     "core24-new",
		"grade":    "dangerous",
		"revision": "1",
		"snaps": []interface{}{
			map[string]interface{}{
				// switch to a new kernel which also is already
				// installed
				"name":            "pc-kernel-new",
				"id":              snaptest.AssertedSnapID("pc-kernel-new"),
				"type":            "kernel",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "pc-new",
				"id":              snaptest.AssertedSnapID("pc-new"),
				"type":            "gadget",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "app-snap",
				"id":              snaptest.AssertedSnapID("app-snap"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})

	chg, err := devicestate.Remodel(s.state, new, []*snap.SideInfo{appSnap}, []string{appSnapPath}, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()

	for _, t := range tl {
		c.Logf("%s: %s", t.Kind(), t.Summary())
	}

	// 3 snaps (2 tasks for each) + assets update and setup from kernel + gadget (3 tasks) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 3*2+2+3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tSwitchKernel := tl[0]
	tSetupKernelSnap := tl[1]
	tUpdateAssetsKernel := tl[2]
	tLinkKernel := tl[3]
	tPrepareBase := tl[4]
	tLinkBase := tl[5]
	tSwitchGadget := tl[6]
	tUpdateAssets := tl[7]
	tUpdateCmdline := tl[8]
	tValidateApp := tl[9]
	tInstallApp := tl[10]
	tCreateRecovery := tl[11]
	tFinalizeRecovery := tl[12]
	tSetModel := tl[13]

	// check the tasks
	c.Assert(tSwitchKernel.Kind(), Equals, "switch-snap")
	c.Assert(tSwitchKernel.Summary(), Equals, `Switch snap "pc-kernel-new" from channel "20/stable" to "20/edge"`)
	c.Assert(tSwitchKernel.WaitTasks(), HasLen, 0)
	c.Assert(tSetupKernelSnap.Kind(), Equals, "setup-kernel-snap")
	c.Assert(tSetupKernelSnap.Summary(), Equals, `Setup kernel driver tree for "pc-kernel-new" (222) for remodel`)
	c.Assert(tLinkKernel.Kind(), Equals, "link-snap")
	c.Assert(tLinkKernel.Summary(), Equals, `Make snap "pc-kernel-new" (222) available to the system during remodel`)
	c.Assert(tUpdateAssetsKernel.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssetsKernel.Summary(), Equals, `Update assets from kernel "pc-kernel-new" (222) for remodel`)
	c.Assert(tPrepareBase.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareBase.Summary(), Equals, `Prepare snap "core24-new" (222) for remodel`)
	c.Assert(tPrepareBase.WaitTasks(), HasLen, 1)
	c.Assert(tLinkBase.Kind(), Equals, "link-snap")
	c.Assert(tLinkBase.Summary(), Equals, `Make snap "core24-new" (222) available to the system during remodel`)
	c.Assert(tSwitchGadget.Kind(), Equals, "switch-snap")
	c.Assert(tSwitchGadget.Summary(), Equals, `Switch snap "pc-new" from channel "20/stable" to "20/edge"`)
	c.Assert(tSwitchGadget.WaitTasks(), HasLen, 1)
	c.Assert(tUpdateAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssets.Summary(), Equals, `Update assets from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateAssets.WaitTasks(), HasLen, 2)
	c.Assert(tUpdateCmdline.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateCmdline.Summary(), Equals, `Update kernel command line from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateCmdline.WaitTasks(), HasLen, 1)
	c.Assert(tValidateApp.Kind(), Equals, "validate-snap")
	c.Assert(tValidateApp.Summary(), Equals, "Validate app-snap")
	c.Assert(tInstallApp.Kind(), Equals, "fake-install")
	c.Assert(tInstallApp.Summary(), Equals, "Install app-snap")
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// check the ordering, prepare/link are part of download edge and come first
	c.Assert(tSwitchKernel.WaitTasks(), HasLen, 0)
	c.Assert(tLinkKernel.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssetsKernel,
	})
	c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchKernel,
		tValidateApp,
		tCreateRecovery,
		tFinalizeRecovery,
	})
	c.Assert(tUpdateAssetsKernel.WaitTasks(), DeepEquals, []*state.Task{
		tSetupKernelSnap,
	})
	c.Assert(tPrepareBase.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchKernel,
	})
	c.Assert(tLinkBase.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
		tLinkKernel,
	})
	c.Assert(tSwitchGadget.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
	})
	c.Assert(tUpdateAssets.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchGadget,
		tLinkBase,
	})
	c.Assert(tUpdateCmdline.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssets,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain (in this case, validate the locally
		// provided snap)
		tValidateApp,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (see above)
		tValidateApp,
	})
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchKernel, tSetupKernelSnap, tUpdateAssetsKernel,
		tLinkKernel, tPrepareBase, tLinkBase,
		tSwitchGadget, tUpdateAssets, tUpdateCmdline,
		tValidateApp, tInstallApp,
		tCreateRecovery, tFinalizeRecovery,
	})
	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tSwitchKernel.ID(), tPrepareBase.ID(), tSwitchGadget.ID(), tValidateApp.ID()},
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnaps(c *C) {
	// remodel switches to a new set of kernel, base and gadget snaps, but
	// those happen to be already installed and tracking the right channels,
	// this scenario can happen when the system has gone through many
	// remodels and the new gadget, kernel, base snaps were required by one
	// of the prior models
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting updated
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core24",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// new gadget, base and kernel which are already installed
	for _, alreadyInstalledName := range []string{"pc-new", "pc-kernel-new", "core24-new"} {
		snapYaml := "name: pc-kernel-new\nversion: 1\ntype: kernel\n"
		channel := "20/stable"
		switch alreadyInstalledName {
		case "core24-new":
			snapYaml = "name: core24-new\nversion: 1\ntype: base\n"
			channel = "latest/stable"
		case "pc-new":
			snapYaml = "name: pc-new\nversion: 1\ntype: gadget\nbase: core24-new\n"
		}
		si := &snap.SideInfo{
			RealName: alreadyInstalledName,
			Revision: snap.R(222),
			SnapID:   snaptest.AssertedSnapID(alreadyInstalledName),
		}
		info := snaptest.MakeSnapFileAndDir(c, snapYaml, nil, si)
		snapstate.Set(s.state, alreadyInstalledName, &snapstate.SnapState{
			SnapType:        string(info.Type()),
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			Active:          true,
			TrackingChannel: channel,
		})
	}

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		// switch to a new base which is already installed
		"base":     "core24-new",
		"grade":    "dangerous",
		"revision": "1",
		"snaps": []interface{}{
			map[string]interface{}{
				// switch to a new kernel which also is already
				// installed
				"name":            "pc-kernel-new",
				"id":              snaptest.AssertedSnapID("pc-kernel-new"),
				"type":            "kernel",
				"default-channel": "20/stable",
			},
			map[string]interface{}{
				"name":            "pc-new",
				"id":              snaptest.AssertedSnapID("pc-new"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 2 snaps (2 tasks for each) + assets update and setup from kernel + gadget (3 tasks) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 2*2+2+3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tPrepareKernel := tl[0]
	tSetupKernelSnap := tl[1]
	tUpdateAssetsKernel := tl[2]
	tLinkKernel := tl[3]
	tPrepareBase := tl[4]
	tLinkBase := tl[5]
	tPrepareGadget := tl[6]
	tUpdateAssets := tl[7]
	tUpdateCmdline := tl[8]
	tCreateRecovery := tl[9]
	tFinalizeRecovery := tl[10]
	tSetModel := tl[11]

	// check the tasks
	c.Assert(tPrepareKernel.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareKernel.Summary(), Equals, `Prepare snap "pc-kernel-new" (222) for remodel`)
	c.Assert(tPrepareKernel.WaitTasks(), HasLen, 0)
	c.Assert(tSetupKernelSnap.Kind(), Equals, "setup-kernel-snap")
	c.Assert(tSetupKernelSnap.Summary(), Equals, `Setup kernel driver tree for "pc-kernel-new" (222) for remodel`)
	c.Assert(tLinkKernel.Kind(), Equals, "link-snap")
	c.Assert(tLinkKernel.Summary(), Equals, `Make snap "pc-kernel-new" (222) available to the system during remodel`)
	c.Assert(tUpdateAssetsKernel.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssetsKernel.Summary(), Equals, `Update assets from kernel "pc-kernel-new" (222) for remodel`)
	c.Assert(tPrepareBase.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareBase.Summary(), Equals, `Prepare snap "core24-new" (222) for remodel`)
	c.Assert(tPrepareBase.WaitTasks(), HasLen, 1)
	c.Assert(tLinkBase.Kind(), Equals, "link-snap")
	c.Assert(tLinkBase.Summary(), Equals, `Make snap "core24-new" (222) available to the system during remodel`)
	c.Assert(tPrepareGadget.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepareGadget.Summary(), Equals, `Prepare snap "pc-new" (222) for remodel`)
	c.Assert(tPrepareGadget.WaitTasks(), HasLen, 1)
	c.Assert(tUpdateAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssets.Summary(), Equals, `Update assets from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateAssets.WaitTasks(), HasLen, 2)
	c.Assert(tUpdateCmdline.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateCmdline.Summary(), Equals, `Update kernel command line from gadget "pc-new" (222) for remodel`)
	c.Assert(tUpdateCmdline.WaitTasks(), HasLen, 1)
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// check the ordering, prepare/link are part of download edge and come first
	c.Assert(tPrepareKernel.WaitTasks(), HasLen, 0)
	c.Assert(tLinkKernel.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssetsKernel,
	})
	c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel,
		tPrepareGadget,
		tCreateRecovery,
		tFinalizeRecovery,
	})
	c.Assert(tUpdateAssetsKernel.WaitTasks(), DeepEquals, []*state.Task{
		tSetupKernelSnap,
	})
	c.Assert(tPrepareBase.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel,
	})
	c.Assert(tLinkBase.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
		tLinkKernel,
	})
	c.Assert(tPrepareGadget.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareBase,
	})
	c.Assert(tUpdateAssets.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareGadget,
		tLinkBase,
	})
	c.Assert(tUpdateCmdline.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssets,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain (in this case prepare & link
		// for existing snaps)
		tPrepareGadget,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (see above)
		tPrepareGadget,
	})
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tPrepareKernel, tSetupKernelSnap, tUpdateAssetsKernel,
		tLinkKernel, tPrepareBase, tLinkBase,
		tPrepareGadget, tUpdateAssets, tUpdateCmdline,
		tCreateRecovery, tFinalizeRecovery,
	})
	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tPrepareKernel.ID(), tPrepareBase.ID(), tPrepareGadget.ID()},
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnapsDifferentChannelThanNew(c *C) {
	s.testRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnapsDifferentChannelThanNew(
		c, &switchDifferentChannelThanNew{})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnapsDifferentChannelThanNewLocal(c *C) {
	s.testRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnapsDifferentChannelThanNew(
		c, &switchDifferentChannelThanNew{localSnaps: true})
}

type switchDifferentChannelThanNew struct {
	localSnaps bool
}

func (s *deviceMgrRemodelSuite) testRemodelUC20SwitchKernelBaseGadgetSnapsInstalledSnapsDifferentChannelThanNew(
	c *C, opts *switchDifferentChannelThanNew) {
	// kernel, base and gadget snaps that are used by the new model are
	// already installed, but track a different channel from what is set in
	// the new model
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	callsToMockedUpdate := 0
	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Assert(strutil.ListContains([]string{"core24-new", "pc-kernel-new", "pc-new"}, name), Equals, true,
			Commentf("unexpected snap %q", name))
		callsToMockedUpdate++
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)

		// pretend the new channel has the same revision, so update is a
		// simple channel switch
		tSwitchChannel := s.state.NewTask("switch-snap-channel", fmt.Sprintf("Switch %s channel to %s", name, opts.Channel))
		typ := "kernel"
		rev := snap.R(222)
		if name == "core24-new" {
			typ = "base"
			rev = snap.R(223)
		} else if name == "pc-new" {
			typ = "gadget"
			rev = snap.R(224)
		}
		tSwitchChannel.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
				Revision: rev,
				SnapID:   snaptest.AssertedSnapID(name),
			},
			Flags: snapstate.Flags{}.ForSnapSetup(),
			Type:  snap.Type(typ),
		})
		ts := state.NewTaskSet(tSwitchChannel)
		// no download-and-checks-done edge
		return ts, nil
	})
	defer restore()

	callsToMockedUpdatePath := 0
	restore = devicestate.MockSnapstateUpdatePathWithDeviceContext(func(st *state.State, si *snap.SideInfo, path, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		callsToMockedUpdatePath++
		c.Assert(strutil.ListContains([]string{"core24-new", "pc-kernel-new", "pc-new"}, name), Equals, true,
			Commentf("unexpected snap %q", name))
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(si, NotNil)
		c.Check(si.RealName, Equals, name)

		// switch channel using SideInfo from the local snap
		tSwitchChannel := s.state.NewTask("switch-snap-channel", fmt.Sprintf("Switch %s channel to %s", name, opts.Channel))
		typ := "kernel"
		if name == "core24-new" {
			typ = "base"
		} else if name == "pc-new" {
			typ = "gadget"
		}
		tSwitchChannel.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: si,
			Flags:    snapstate.Flags{}.ForSnapSetup(),
			Type:     snap.Type(typ),
		})
		ts := state.NewTaskSet(tSwitchChannel)
		// no download-and-checks-done edge
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core24",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// new gadget and kernel which are already installed
	for _, alreadyInstalledName := range []string{"pc-kernel-new", "core24-new", "pc-new"} {
		snapYaml := "name: pc-kernel-new\nversion: 1\ntype: kernel\n"
		channel := "other/edge"
		if alreadyInstalledName == "core24-new" {
			snapYaml = "name: core24-new\nversion: 1\ntype: base\n"
		} else if alreadyInstalledName == "pc-new" {
			snapYaml = "name: pc-new\nversion: 1\ntype: gadget\nbase: core24-new\n"
		}
		si := &snap.SideInfo{
			RealName: alreadyInstalledName,
			Revision: snap.R(222),
			SnapID:   snaptest.AssertedSnapID(alreadyInstalledName),
		}
		info := snaptest.MakeSnapFileAndDir(c, snapYaml, nil, si)
		snapstate.Set(s.state, alreadyInstalledName, &snapstate.SnapState{
			SnapType:        string(info.Type()),
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			Active:          true,
			TrackingChannel: channel,
		})
	}

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		// switch to a new base which is already installed
		"base":     "core24-new",
		"grade":    "dangerous",
		"revision": "1",
		"snaps": []interface{}{
			map[string]interface{}{
				// switch to a new kernel which also is already
				// installed, but tracks a different channel
				// than what we have in snap state
				"name":            "pc-kernel-new",
				"id":              snaptest.AssertedSnapID("pc-kernel-new"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc-new",
				"id":              snaptest.AssertedSnapID("pc-new"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				// similar case for the base snap
				"name":            "core24-new",
				"id":              snaptest.AssertedSnapID("core24-new"),
				"type":            "base",
				"default-channel": "latest/stable",
			},
		},
	})

	var localSnaps []*snap.SideInfo
	var paths []string
	if opts.localSnaps {
		for i, name := range []string{"pc-kernel-new", "core24-new", "pc-new"} {
			si, path := createLocalSnap(c, name, snaptest.AssertedSnapID(name), 222+i, "", "", nil)
			localSnaps = append(localSnaps, si)
			paths = append(paths, path)
		}
	}

	chg, err := devicestate.Remodel(s.state, new, localSnaps, paths, devicestate.RemodelOptions{
		Offline: opts.localSnaps,
	})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")
	if opts.localSnaps {
		c.Check(callsToMockedUpdate, Equals, 0)
		c.Check(callsToMockedUpdatePath, Equals, 3)
	} else {
		c.Check(callsToMockedUpdate, Equals, 3)
		c.Check(callsToMockedUpdatePath, Equals, 0)
	}

	tl := chg.Tasks()
	// 2 snaps with (snap switch channel + link snap) + assets update and setup
	// for the kernel snap + gadget snap (switch channel, assets update, cmdline update) +
	// recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 2*2+2+3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tSwitchChannelKernel := tl[0]
	tSetupKernelSnap := tl[1]
	tUpdateAssetsFromKernel := tl[2]
	tLinkKernel := tl[3]
	tSwitchChannelBase := tl[4]
	tLinkBase := tl[5]
	tSwitchChannelGadget := tl[6]
	tUpdateAssetsFromGadget := tl[7]
	tUpdateCmdlineFromGadget := tl[8]
	tCreateRecovery := tl[9]
	tFinalizeRecovery := tl[10]
	tSetModel := tl[11]

	// check the tasks
	c.Assert(tSwitchChannelKernel.Kind(), Equals, "switch-snap-channel")
	c.Assert(tSwitchChannelKernel.Summary(), Equals, `Switch pc-kernel-new channel to 20/stable`)
	c.Assert(tSwitchChannelKernel.WaitTasks(), HasLen, 0)
	c.Assert(tSetupKernelSnap.Kind(), Equals, "setup-kernel-snap")
	c.Assert(tSetupKernelSnap.Summary(), Equals, `Setup kernel driver tree for "pc-kernel-new" (222) for remodel`)
	c.Assert(tUpdateAssetsFromKernel.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssetsFromKernel.Summary(), Equals, `Update assets from kernel "pc-kernel-new" (222) for remodel`)
	c.Assert(tLinkKernel.Kind(), Equals, "link-snap")
	c.Assert(tLinkKernel.Summary(), Equals, `Make snap "pc-kernel-new" (222) available to the system during remodel`)
	c.Assert(tSwitchChannelBase.Kind(), Equals, "switch-snap-channel")
	c.Assert(tSwitchChannelBase.Summary(), Equals, `Switch core24-new channel to latest/stable`)
	c.Assert(tLinkBase.Kind(), Equals, "link-snap")
	c.Assert(tLinkBase.Summary(), Equals, `Make snap "core24-new" (223) available to the system during remodel`)
	c.Assert(tSwitchChannelGadget.Kind(), Equals, "switch-snap-channel")
	c.Assert(tSwitchChannelGadget.Summary(), Equals, `Switch pc-new channel to 20/stable`)
	c.Assert(tUpdateAssetsFromGadget.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateAssetsFromGadget.Summary(), Equals, `Update assets from gadget "pc-new" (224) for remodel`)
	c.Assert(tUpdateCmdlineFromGadget.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateCmdlineFromGadget.Summary(), Equals, `Update kernel command line from gadget "pc-new" (224) for remodel`)
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// check the ordering, prepare/link are part of download edge and come first
	c.Assert(tSwitchChannelKernel.WaitTasks(), HasLen, 0)
	c.Assert(tSwitchChannelBase.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelKernel,
	})
	c.Assert(tSwitchChannelGadget.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelBase,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelGadget,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		tSwitchChannelGadget,
	})
	c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelKernel, tSwitchChannelGadget,
		tCreateRecovery, tFinalizeRecovery,
	})
	c.Assert(tUpdateAssetsFromKernel.WaitTasks(), DeepEquals, []*state.Task{
		tSetupKernelSnap,
	})
	c.Check(tLinkKernel.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateAssetsFromKernel,
	})
	c.Assert(tLinkBase.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelBase, tLinkKernel,
	})
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannelKernel, tSetupKernelSnap, tUpdateAssetsFromKernel,
		tLinkKernel, tSwitchChannelBase, tLinkBase,
		tSwitchChannelGadget, tUpdateAssetsFromGadget, tUpdateCmdlineFromGadget,
		tCreateRecovery, tFinalizeRecovery,
	})
	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":     expectedLabel,
		"directory": filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		// tasks carrying snap-setup are tracked
		"snap-setup-tasks": []interface{}{
			tSwitchChannelKernel.ID(),
			tSwitchChannelBase.ID(),
			tSwitchChannelGadget.ID(),
		},
		"test-system": true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20SwitchKernelBaseSnapsInstalledSnapsWithUpdates(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// new gadget and kernel which are already installed
	for _, alreadyInstalledName := range []string{"pc-kernel-new", "core20-new"} {
		snapYaml := "name: pc-kernel-new\nversion: 1\ntype: kernel\n"
		channel := "kernel/stable"
		if alreadyInstalledName == "core20-new" {
			snapYaml = "name: core20-new\nversion: 1\ntype: base\n"
			channel = "base/stable"
		}
		si := &snap.SideInfo{
			RealName: alreadyInstalledName,
			Revision: snap.R(222),
			SnapID:   snaptest.AssertedSnapID(alreadyInstalledName),
		}
		info := snaptest.MakeSnapFileAndDir(c, snapYaml, nil, si)
		snapstate.Set(s.state, alreadyInstalledName, &snapstate.SnapState{
			SnapType:        string(info.Type()),
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			Active:          true,
			TrackingChannel: channel,
		})
	}

	// new kernel and base are already installed, but using a different channel
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		// switch to a new base which is already installed
		"base":     "core20-new",
		"grade":    "dangerous",
		"revision": "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel-new",
				"id":              snaptest.AssertedSnapID("pc-kernel-new"),
				"type":            "kernel",
				"default-channel": "kernel/edge",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "core20-new",
				"id":              snaptest.AssertedSnapID("core20-new"),
				"type":            "base",
				"default-channel": "base/edge",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 2 snaps (3 tasks for each) + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 2*3+2+1)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tDownloadKernel := tl[0]
	tValidateKernel := tl[1]
	tInstallKernel := tl[2]
	tDownloadBase := tl[3]
	tValidateBase := tl[4]
	tInstallBase := tl[5]
	tCreateRecovery := tl[6]
	tFinalizeRecovery := tl[7]
	tSetModel := tl[8]

	// check the tasks
	expectedLabel := now.Format("20060102")
	c.Assert(tDownloadKernel.Kind(), Equals, "fake-download")
	c.Assert(tDownloadKernel.Summary(), Equals, "Download pc-kernel-new from track kernel/edge")
	c.Assert(tDownloadKernel.WaitTasks(), HasLen, 0)
	c.Assert(tValidateKernel.Kind(), Equals, "validate-snap")
	c.Assert(tValidateKernel.Summary(), Equals, "Validate pc-kernel-new")
	c.Assert(tDownloadBase.Kind(), Equals, "fake-download")
	c.Assert(tDownloadBase.Summary(), Equals, "Download core20-new from track base/edge")
	c.Assert(tDownloadBase.WaitTasks(), HasLen, 1)
	c.Assert(tValidateBase.Kind(), Equals, "validate-snap")
	c.Assert(tValidateBase.Summary(), Equals, "Validate core20-new")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// check the ordering, prepare/link are part of download edge and come first
	c.Assert(tDownloadKernel.WaitTasks(), HasLen, 0)
	c.Assert(tValidateKernel.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadKernel,
	})
	c.Assert(tInstallKernel.WaitTasks(), DeepEquals, []*state.Task{
		tValidateKernel,
		tValidateBase,
		// wait for recovery system to be created
		tCreateRecovery,
		// and then finalized
		tFinalizeRecovery,
	})
	c.Assert(tInstallBase.WaitTasks(), DeepEquals, []*state.Task{
		tValidateBase,
		// previous install chain
		tInstallKernel,
	})
	c.Assert(tCreateRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// last snap of the download chain
		tValidateBase,
	})
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
		// last snap of the download chain (added later)
		tValidateBase,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tDownloadKernel, tValidateKernel, tInstallKernel,
		tDownloadBase, tValidateBase, tInstallBase,
		tCreateRecovery, tFinalizeRecovery,
	})

	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": []interface{}{tDownloadKernel.ID(), tDownloadBase.ID()},
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20EssentialSnapsTrackingDifferentChannelThanDefaultSameAsNew(c *C) {
	// essential snaps from new model are already installed and track
	// channels different than declared in the old model, but already the
	// same as in the new one
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting updated
		return nil, fmt.Errorf("unexpected update call")
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		return nil, fmt.Errorf("unexpected install call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// base, kernel & gadget snaps already track the default channels
	// declared in new model

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/edge",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/edge",
	})
	// current base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "20/edge",
	})

	// new kernel and base are already installed, but using a different channel
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "20/edge",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 3)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tCreateRecovery := tl[0]
	tFinalizeRecovery := tl[1]
	tSetModel := tl[2]

	// check the tasks
	expectedLabel := now.Format("20060102")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	c.Assert(tCreateRecovery.WaitTasks(), HasLen, 0)
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tCreateRecovery, tFinalizeRecovery,
	})

	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": nil,
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelFailWhenUsingUnassertedSnapForSpecificRevision(c *C) {
	// remodel when the essential snaps declared in new model are already
	// installed, but have a local revision
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting updated
		return nil, fmt.Errorf("unexpected update call")
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		return nil, fmt.Errorf("unexpected install call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// base, kernel & gadget snaps are already present but are unasserted
	// and have local revisions

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(-33),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(-32),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "",
	})
	// current base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(-32),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "",
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       snaptest.AssertedSnapID("pc-kernel"),
				"revision": "10",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	err = s.storeSigning.Add(vset)
	c.Assert(err, IsNil)

	// new kernel and base are already installed, but kernel needs a new
	// revision and base is a new channel
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "20/edge",
			},
		},
	})

	_, err = devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, "cannot determine if unasserted snap revision matches required revision")
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20BaseNoDownloadSimpleChannelSwitch(c *C) {
	// remodel when a channel declared in new model carries the same
	// revision as already installed, so there is no full fledged, but a
	// simple channel switch
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// expecting an update call for the base snap
		c.Assert(name, Equals, "core20")
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)

		tSwitchChannel := s.state.NewTask("switch-snap-channel", fmt.Sprintf("Switch %s channel to %s", name, opts.Channel))
		ts := state.NewTaskSet(tSwitchChannel)
		// no download-and-checks-done edge
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		return nil, fmt.Errorf("unexpected install call")
	})
	defer restore()

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:  siModelBase.Revision,
		Active:   true,
		// the same channel as in the current model
		TrackingChannel: "latest/stable",
	})

	// base uses a new default channel
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "latest/edge",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 1 switch channel + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 4)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tSwitchChannel := tl[0]
	tCreateRecovery := tl[1]
	tFinalizeRecovery := tl[2]
	tSetModel := tl[3]

	// check the tasks
	expectedLabel := now.Format("20060102")
	// added by mock
	c.Assert(tSwitchChannel.Kind(), Equals, "switch-snap-channel")
	c.Assert(tSwitchChannel.Summary(), Equals, "Switch core20 channel to latest/edge")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", expectedLabel))
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tFinalizeRecovery.Summary(), Equals, fmt.Sprintf("Finalize recovery system with label %q", expectedLabel))
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	c.Assert(tCreateRecovery.WaitTasks(), HasLen, 0)
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
	})

	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tSetModel.Summary(), Equals, "Set new model assertion")
	// setModel waits for everything in the change
	c.Assert(tSetModel.WaitTasks(), DeepEquals, []*state.Task{
		tSwitchChannel, tCreateRecovery, tFinalizeRecovery,
	})

	// verify recovery system setup data on appropriate tasks
	var systemSetupData map[string]interface{}
	err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            expectedLabel,
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", expectedLabel),
		"snap-setup-tasks": nil,
		"test-system":      true,
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20EssentialNoDownloadSimpleChannelSwitch(c *C) {
	// remodel when a non-essential snap in the new model specifies a new
	// channel, but the revision is already installed. so there is no full
	// fledged install, but a simple channel switch
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// expecting an update call for the base snap
		c.Assert(name, Equals, "snap-1")
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)

		tSwitchChannel := s.state.NewTask("switch-snap-channel", fmt.Sprintf("Switch %s channel to %s", name, opts.Channel))
		ts := state.NewTaskSet(tSwitchChannel)
		// no download-and-checks-done edge
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// no snaps are getting installed
		return nil, fmt.Errorf("unexpected install call")
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "snap-1-base",
				"id":              snaptest.AssertedSnapID("snap-1-base"),
				"type":            "base",
				"default-channel": "latest/stable",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	// current snap-1-base
	appBase := &snap.SideInfo{
		RealName: "snap-1-base",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("snap-1-base"),
	}
	snapstate.Set(s.state, "snap-1-base", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{appBase}),
		Current:         appBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	// current snap-1 app
	appSnapBase := &snap.SideInfo{
		RealName: "snap-1",
		Revision: snap.R(12),
		SnapID:   snaptest.AssertedSnapID("snap-1"),
	}

	const appYaml = `
name: snap-1
version: 1
base: snap-1-base
`

	info := snaptest.MakeSnapFileAndDir(c, appYaml, nil, appSnapBase)

	snapstate.Set(s.state, "snap-1", &snapstate.SnapState{
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&info.SideInfo}),
		Current:  info.Revision,
		Active:   true,
		// the same channel as in the current model
		TrackingChannel: "latest/stable",
	})

	// snap-1 uses a new default channel
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/edge",
			},
			map[string]interface{}{
				"name":            "snap-1-base",
				"id":              snaptest.AssertedSnapID("snap-1-base"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})
	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	c.Assert(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tl := chg.Tasks()
	// 1 switch channel + recovery system (2 tasks) + set-model
	c.Assert(tl, HasLen, 4)

	deviceCtx, err := devicestate.DeviceCtx(s.state, tl[0], nil)
	c.Assert(err, IsNil)
	// deviceCtx is actually a remodelContext here
	remodCtx, ok := deviceCtx.(devicestate.RemodelContext)
	c.Assert(ok, Equals, true)
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	c.Check(remodCtx.Kind(), Equals, devicestate.UpdateRemodel)
	c.Check(remodCtx.Model(), DeepEquals, new)
	c.Check(remodCtx.Store(), IsNil)

	// check the tasks
	tSwitchChannel := tl[0]
	tCreateRecovery := tl[1]
	tFinalizeRecovery := tl[2]
	tSetModel := tl[3]

	// check the tasks
	c.Assert(tSwitchChannel.Kind(), Equals, "switch-snap-channel")
	c.Assert(tSwitchChannel.Summary(), Equals, "Switch snap-1 channel to latest/edge")
	c.Assert(tCreateRecovery.Kind(), Equals, "create-recovery-system")
	c.Assert(tFinalizeRecovery.Kind(), Equals, "finalize-recovery-system")
	c.Assert(tSetModel.Kind(), Equals, "set-model")
	c.Assert(tCreateRecovery.WaitTasks(), HasLen, 0)
	c.Assert(tFinalizeRecovery.WaitTasks(), DeepEquals, []*state.Task{
		// recovery system being created
		tCreateRecovery,
	})
}

type remodelUC20LabelConflictsTestCase struct {
	now              time.Time
	breakPermissions bool
	expectedErr      string
}

func (s *deviceMgrRemodelSuite) testRemodelUC20LabelConflicts(c *C, tc remodelUC20LabelConflictsTestCase) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Errorf("unexpected call, test broken")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	restore = devicestate.MockTimeNow(func() time.Time { return tc.now })
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	labelBase := tc.now.Format("20060102")
	// create a conflict with base label
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", labelBase), 0755)
	c.Assert(err, IsNil)
	for i := 0; i < 5; i++ {
		// create conflicting labels with numerical suffices
		l := fmt.Sprintf("%s-%d", labelBase, i)
		err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", l), 0755)
		c.Assert(err, IsNil)
	}
	// and some confusing labels
	for _, suffix := range []string{"--", "-abc", "-abc-1", "foo", "-"} {
		err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", labelBase+suffix), 0755)
		c.Assert(err, IsNil)
	}
	// and a label that will force a max number
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", labelBase+"-990"), 0755)
	c.Assert(err, IsNil)

	if tc.breakPermissions {
		systemsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems")
		c.Assert(os.Chmod(systemsDir, 0000), IsNil)
		defer os.Chmod(systemsDir, 0755)
	}

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	if tc.expectedErr == "" {
		c.Assert(err, IsNil)
		c.Assert(chg, NotNil)

		var tCreateRecovery *state.Task
		for _, tsk := range chg.Tasks() {
			if tsk.Kind() == "create-recovery-system" {
				tCreateRecovery = tsk
				break
			}
		}
		happyLabel := labelBase + "-991"
		c.Assert(tCreateRecovery, NotNil)
		c.Assert(tCreateRecovery.Summary(), Equals, fmt.Sprintf("Create recovery system with label %q", happyLabel))
		var systemSetupData map[string]interface{}
		err = tCreateRecovery.Get("recovery-system-setup", &systemSetupData)
		c.Assert(err, IsNil)
		c.Assert(systemSetupData["label"], Equals, happyLabel)
	} else {
		c.Assert(err, ErrorMatches, tc.expectedErr)
		c.Assert(chg, IsNil)
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20LabelConflictsHappy(c *C) {
	now := time.Now()
	s.testRemodelUC20LabelConflicts(c, remodelUC20LabelConflictsTestCase{now: now})
}

func (s *deviceMgrRemodelSuite) TestRemodelUC20LabelConflictsError(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be executed by the root user")
	}
	now := time.Now()
	nowLabel := now.Format("20060102")
	s.testRemodelUC20LabelConflicts(c, remodelUC20LabelConflictsTestCase{
		now:              now,
		breakPermissions: true,
		expectedErr:      fmt.Sprintf(`cannot select non-conflicting label for recovery system "%[1]s": stat .*/run/mnt/ubuntu-seed/systems/%[1]s: permission denied`, nowLabel),
	})
}

type uc20RemodelSetModelTestCase struct {
	// errors on consecutive reseals
	resealErr    []error
	taskLogMatch string
	logMatch     string
}

func (s *deviceMgrRemodelSuite) testUC20RemodelSetModel(c *C, tc uc20RemodelSetModelTestCase) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	devicestate.SetBootOkRan(s.mgr, true)
	devicestate.SetBootRevisionsUpdated(s.mgr, true)

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)

	s.mockTasksNopHandler("fake-download", "validate-snap", "fake-install",
		// create recovery system requests are boot, which is not done here
		"create-recovery-system", "finalize-recovery-system")

	// set a model assertion we remodel from
	model := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	oldSeededTs := time.Now().AddDate(0, 0, -1)
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Revision:  model.Revision(),
			Timestamp: model.Timestamp(),
			SeedTime:  oldSeededTs,
		},
	})
	s.state.Set("default-recovery-system", devicestate.DefaultRecoverySystem{
		System:          "0000",
		Model:           model.Model(),
		BrandID:         model.BrandID(),
		Timestamp:       model.Timestamp(),
		Revision:        model.Revision(),
		TimeMadeDefault: oldSeededTs,
	})
	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	// the target model
	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	buf, restore := logger.MockLogger()
	defer restore()

	m := boot.Modeenv{
		Mode: "run",

		GoodRecoverySystems:    []string{"0000"},
		CurrentRecoverySystems: []string{"0000"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(m.WriteTo(""), IsNil)

	now := time.Now()
	expectedLabel := now.Format("20060102")
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()
	s.state.Set("tried-systems", []string{expectedLabel})

	resealKeyCalls := 0
	restore = boot.MockResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeyCalls++
		c.Assert(len(tc.resealErr) >= resealKeyCalls, Equals, true)
		c.Check(modeenv.GoodRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		c.Check(modeenv.CurrentRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		return tc.resealErr[resealKeyCalls-1]
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	var setModelTask *state.Task
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "set-model" {
			setModelTask = tsk
			break
		}
	}
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), IsNil)
	c.Check(resealKeyCalls, Equals, len(tc.resealErr))
	// even if errors occur during reseal, set-model is a point of no return
	c.Check(setModelTask.Status(), Equals, state.DoneStatus)
	var seededSystems []devicestate.SeededSystem
	c.Assert(s.state.Get("seeded-systems", &seededSystems), IsNil)
	hasError := false
	for _, err := range tc.resealErr {
		if err != nil {
			hasError = true
			break
		}
	}
	if !hasError {
		c.Check(setModelTask.Log(), HasLen, 0)

		c.Assert(seededSystems, HasLen, 2)
		// the system was seeded after our mocked 'now' or at the same
		// time if clock resolution is very low, but not before it
		c.Check(seededSystems[0].SeedTime.Before(now), Equals, false)
		// record when the system was seeded
		newSystemSeededTime := seededSystems[0].SeedTime
		seededSystems[0].SeedTime = time.Time{}

		c.Check(seededSystems[1].SeedTime.Equal(oldSeededTs), Equals, true)
		seededSystems[1].SeedTime = time.Time{}
		c.Check(seededSystems, DeepEquals, []devicestate.SeededSystem{
			{
				System:    expectedLabel,
				Model:     new.Model(),
				BrandID:   new.BrandID(),
				Revision:  new.Revision(),
				Timestamp: new.Timestamp(),
			},
			{
				System:    "0000",
				Model:     model.Model(),
				BrandID:   model.BrandID(),
				Revision:  model.Revision(),
				Timestamp: model.Timestamp(),
			},
		})

		var defaultSystem devicestate.DefaultRecoverySystem
		c.Assert(s.state.Get("default-recovery-system", &defaultSystem), IsNil)
		// // check that the timestamp is not empty and clear it, so that
		// // the comparison below works
		c.Check(defaultSystem.TimeMadeDefault.Equal(newSystemSeededTime), Equals, true)
		defaultSystem.TimeMadeDefault = time.Time{}
		c.Check(defaultSystem, Equals, devicestate.DefaultRecoverySystem{
			System:    expectedLabel,
			Model:     new.Model(),
			BrandID:   new.BrandID(),
			Revision:  new.Revision(),
			Timestamp: new.Timestamp(),
		})
	} else {
		// however, error is still logged, both to the task and the logger
		c.Check(strings.Join(setModelTask.Log(), "\n"), Matches, tc.taskLogMatch)
		c.Check(buf.String(), Matches, tc.logMatch)

		c.Assert(seededSystems, HasLen, 1)
		c.Check(seededSystems[0].SeedTime.Equal(oldSeededTs), Equals, true)
		seededSystems[0].SeedTime = time.Time{}
		c.Check(seededSystems, DeepEquals, []devicestate.SeededSystem{
			{
				System:    "0000",
				Model:     model.Model(),
				BrandID:   model.BrandID(),
				Timestamp: model.Timestamp(),
				Revision:  model.Revision(),
			},
		})

		var defaultSystem devicestate.DefaultRecoverySystem
		c.Assert(s.state.Get("default-recovery-system", &defaultSystem), IsNil)
		// check that the timestamp is not empty and clear it, so that
		// the comparison below works
		c.Check(defaultSystem.TimeMadeDefault.Equal(oldSeededTs), Equals, true)
		defaultSystem.TimeMadeDefault = time.Time{}
		c.Check(defaultSystem, Equals, devicestate.DefaultRecoverySystem{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			Revision:  model.Revision(),
		})
	}
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialInstall(c *C) {
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: false})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialUpdate(c *C) {
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: true})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialInstallNoSerial(c *C) {
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: false, noSerial: true})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialUpdateNoSerial(c *C) {
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: true, noSerial: true})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialInstallExtraSnap(c *C) {
	// We check that it is fine to pass down a snap that is not used,
	// although we might change the behavior in the future.
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: false, notUsedSnap: true})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelLocalNonEssentialUpdateExtraSnap(c *C) {
	// We check that it is fine to pass down a snap that is not used,
	// although we might change the behavior in the future.
	s.testUC20RemodelLocalNonEssential(c,
		&uc20RemodelLocalNonEssentialCase{isUpdate: true, notUsedSnap: true})
}

type uc20RemodelLocalNonEssentialCase struct {
	isUpdate    bool
	notUsedSnap bool
	noSerial    bool
}

func (s *deviceMgrRemodelSuite) testUC20RemodelLocalNonEssential(c *C, tc *uc20RemodelLocalNonEssentialCase) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	devicestate.SetBootOkRan(s.mgr, true)
	devicestate.SetBootRevisionsUpdated(s.mgr, true)

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)

	s.mockTasksNopHandler("fake-download", "validate-snap", "fake-install",
		// create recovery system requests are boot, which is not done here
		"create-recovery-system", "finalize-recovery-system")

	// set a model assertion we remodel from
	essentialSnaps := []interface{}{
		map[string]interface{}{
			"name":            "pc-kernel",
			"id":              snaptest.AssertedSnapID("pc-kernel"),
			"type":            "kernel",
			"default-channel": "20",
		},
		map[string]interface{}{
			"name":            "pc",
			"id":              snaptest.AssertedSnapID("pc"),
			"type":            "gadget",
			"default-channel": "20",
		},
	}
	snaps := essentialSnaps
	if tc.isUpdate {
		snaps = append(snaps, map[string]interface{}{
			"name":            "some-snap",
			"id":              snaptest.AssertedSnapID("some-snap"),
			"type":            "app",
			"default-channel": "latest",
		})
	}
	model := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps":        snaps,
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	deviceState := auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	}
	if tc.noSerial {
		deviceState.Serial = ""
		deviceState.KeyID = "device-key-id"
	}
	devicestatetest.SetDevice(s.state, &deviceState)

	oldSeededTs := time.Now().AddDate(0, 0, -1)
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			SeedTime:  oldSeededTs,
		},
	})
	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	// extra snap
	if tc.isUpdate {
		siSomeSnap := &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
			SnapID:   snaptest.AssertedSnapID("some-snap"),
		}
		snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
			SnapType:        "app",
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siSomeSnap}),
			Current:         siSomeSnap.Revision,
			Active:          true,
			TrackingChannel: "latest/stable",
		})
	}

	newModelSnaps := essentialSnaps
	newModelSnaps = append(newModelSnaps, map[string]interface{}{
		"name":            "some-snap",
		"id":              snaptest.AssertedSnapID("some-snap"),
		"type":            "app",
		"default-channel": "new-channel",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps":        newModelSnaps,
	})

	installWithDeviceContextCalled := 0
	restore := devicestate.MockSnapstateInstallPathWithDeviceContext(func(st *state.State, si *snap.SideInfo, path, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		installWithDeviceContextCalled++
		c.Check(si, NotNil)
		c.Check(si.RealName, Equals, name)
		c.Check(si.RealName, Not(Equals), "not-used-snap")

		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.Set("snap-setup",
			&snapstate.SnapSetup{SideInfo: si, Channel: opts.Channel})
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	updateWithDeviceContextCalled := 0
	restore = devicestate.MockSnapstateUpdatePathWithDeviceContext(func(st *state.State, si *snap.SideInfo, path, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		updateWithDeviceContextCalled++
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(si, NotNil)
		c.Check(si.RealName, Equals, name)
		c.Check(si.RealName, Not(Equals), "not-used-snap")

		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.Set("snap-setup",
			&snapstate.SnapSetup{SideInfo: si, Channel: opts.Channel})
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	m := boot.Modeenv{
		Mode: "run",

		GoodRecoverySystems:    []string{"0000"},
		CurrentRecoverySystems: []string{"0000"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(m.WriteTo(""), IsNil)

	now := time.Now()
	expectedLabel := now.Format("20060102")
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()
	s.state.Set("tried-systems", []string{expectedLabel})

	resealKeyCalls := 0
	restore = boot.MockResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeyCalls++
		c.Check(modeenv.GoodRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		c.Check(modeenv.CurrentRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		return nil
	})
	defer restore()

	siSomeSnapNew, path := createLocalSnap(c, "some-snap", snaptest.AssertedSnapID("some-snap"), 3, "app", "", nil)
	localSnaps := []*snap.SideInfo{siSomeSnapNew}
	paths := []string{path}
	if tc.notUsedSnap {
		siNotUsed, pathNotUsed := createLocalSnap(c, "not-used-snap", snaptest.AssertedSnapID("not-used-snap"), 3, "app", "", nil)
		localSnaps = append(localSnaps, siNotUsed)
		paths = append(paths, pathNotUsed)
	}

	chg, err := devicestate.Remodel(s.state, new, localSnaps, paths, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, IsNil)
	if tc.isUpdate {
		c.Check(installWithDeviceContextCalled, Equals, 0)
		c.Check(updateWithDeviceContextCalled, Equals, 1)
	} else {
		c.Check(installWithDeviceContextCalled, Equals, 1)
		c.Check(updateWithDeviceContextCalled, Equals, 0)
	}
	var setModelTask *state.Task
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "set-model" {
			setModelTask = tsk
			break
		}
	}
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), IsNil)
	// even if errors occur during reseal, set-model is a point of no return
	c.Check(setModelTask.Status(), Equals, state.DoneStatus)
	var seededSystems []devicestate.SeededSystem
	c.Assert(s.state.Get("seeded-systems", &seededSystems), IsNil)

	c.Check(setModelTask.Log(), HasLen, 0)

	c.Assert(seededSystems, HasLen, 2)
	// the system was seeded after our mocked 'now' or at the same
	// time if clock resolution is very low, but not before it
	c.Check(seededSystems[0].SeedTime.Before(now), Equals, false)
	seededSystems[0].SeedTime = time.Time{}
	c.Check(seededSystems[1].SeedTime.Equal(oldSeededTs), Equals, true)
	seededSystems[1].SeedTime = time.Time{}
	c.Check(seededSystems, DeepEquals, []devicestate.SeededSystem{
		{
			System:    expectedLabel,
			Model:     new.Model(),
			BrandID:   new.BrandID(),
			Revision:  new.Revision(),
			Timestamp: new.Timestamp(),
		},
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			Revision:  model.Revision(),
		},
	})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelSetModelHappy(c *C) {
	s.testUC20RemodelSetModel(c, uc20RemodelSetModelTestCase{
		resealErr: []error{
			nil, // promote recovery system
			nil, // device change pre model write
			nil, // device change post model write
		},
	})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelSetModelErr(c *C) {
	s.testUC20RemodelSetModel(c, uc20RemodelSetModelTestCase{
		resealErr: []error{
			nil, // promote tried recovery system
			// keep this comment so that gofmt does not complain
			fmt.Errorf("mock reseal error"), // device change pre model write
		},
		taskLogMatch: `.* cannot complete remodel: \[cannot switch device: mock reseal error\]`,
		logMatch:     `(?s).* cannot complete remodel: \[cannot switch device: mock reseal error\].`,
	})
}

func (s *deviceMgrRemodelSuite) TestUC20RemodelSetModelWithReboot(c *C) {
	// check that set-model does the right thing even if it is restarted
	// after an unexpected reboot; this gets complicated as we cannot
	// panic() at a random place in the task runner, so we set up the state
	// such that the set-model task completes once and is re-run again

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	devicestate.SetBootOkRan(s.mgr, true)
	devicestate.SetBootRevisionsUpdated(s.mgr, true)

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		return &freshSessionStore{}
	}

	s.mockTasksNopHandler("fake-download", "validate-snap", "fake-install",
		"check-snap", "request-serial",
		// create recovery system requests are boot, which is not done
		// here
		"create-recovery-system", "finalize-recovery-system")

	// set a model assertion we remodel from
	model := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	writeDeviceModelToUbuntuBoot(c, model)
	// the gadget needs to be mocked
	info := snaptest.MakeSnapFileAndDir(c, "name: pc\nversion: 1\ntype: gadget\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc"),
		Revision: snap.R(1),
		RealName: "pc",
	})
	snapstate.Set(s.state, info.InstanceName(), &snapstate.SnapState{
		SnapType: string(info.Type()),
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&info.SideInfo}),
		Current:  info.Revision,
	})

	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
	oldSeededTs := time.Now().AddDate(0, 0, -1)
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			SeedTime:  oldSeededTs,
		},
	})
	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	// the target model, since it's a new model altogether a reregistration
	// will be triggered
	new := s.brands.Model("canonical", "pc-new-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	m := boot.Modeenv{
		Mode: "run",

		GoodRecoverySystems:    []string{"0000"},
		CurrentRecoverySystems: []string{"0000"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(m.WriteTo(""), IsNil)

	now := time.Now()
	restore = devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()
	expectedLabel := now.Format("20060102")
	s.state.Set("tried-systems", []string{expectedLabel})

	resealKeyCalls := 0
	restore = boot.MockResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, u boot.Unlocker) error {
		resealKeyCalls++
		// calls:
		// 1 - promote recovery system
		// 2 - reseal with both models
		// 3 - reseal with new model as current
		// (mocked reboot)
		// 4 - promote recovery system
		// 5 - reseal with new model as current and try; before reboot
		//     set-model changed the model in the state, the new model
		//     replaced the old one, and thus the remodel context
		//     carries the new model in ground context
		// 6 - reseal with new model as current
		c.Check(modeenv.GoodRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		c.Check(modeenv.CurrentRecoverySystems, DeepEquals, []string{"0000", expectedLabel})
		switch resealKeyCalls {
		case 2:
			c.Check(modeenv.Model, Equals, model.Model())
			c.Check(modeenv.TryModel, Equals, new.Model())
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("model: %s\n", model.Model()))
			// old model's revision is 0
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				Not(testutil.FileContains), "revision:")
		case 3:
			c.Check(modeenv.Model, Equals, new.Model())
			c.Check(modeenv.TryModel, Equals, "")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("model: %s\n", new.Model()))
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("revision: %v\n", new.Revision()))
		case 5:
			c.Check(modeenv.Model, Equals, model.Model())
			c.Check(modeenv.TryModel, Equals, new.Model())
			// we are in an after reboot scenario, the file contains
			// the new model
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("model: %s\n", new.Model()))
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("revision: %v\n", new.Revision()))
			// check unlocker
			u()()
		case 6:
			c.Check(modeenv.Model, Equals, new.Model())
			c.Check(modeenv.TryModel, Equals, "")
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("model: %s\n", new.Model()))
			c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"),
				testutil.FileContains, fmt.Sprintf("revision: %v\n", new.Revision()))
			// check unlocker
			u()()
		}
		if resealKeyCalls > 6 {
			c.Fatalf("unexpected #%v call to reseal key to modeenv", resealKeyCalls)
		}
		return nil
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	// since we cannot panic in random place in code that runs under
	// taskrunner, we reset the task status and retry the change again, but
	// we cannot do that once a change has become ready, thus inject a task
	// that will request a reboot and keep retrying, thus stopping execution
	// and keeping the change in a not ready state
	fakeRebootCalls := 0
	fakeRebootCallsReady := false
	s.o.TaskRunner().AddHandler("fake-reboot-and-stall", func(task *state.Task, _ *tomb.Tomb) error {
		fakeRebootCalls++
		if fakeRebootCalls == 1 {
			st := task.State()
			st.Lock()
			defer st.Unlock()
			// not strictly needed, but underlines there's a reboot
			// happening
			restart.Request(st, restart.RestartSystemNow, nil)
		}
		if fakeRebootCallsReady {
			return nil
		}
		// we're not ready, so that the change does not complete yet
		return &state.Retry{}
	}, nil)
	fakeRebootTask := s.state.NewTask("fake-reboot-and-stall", "fake reboot and stalling injected by tests")
	chg.AddTask(fakeRebootTask)
	var setModelTask *state.Task
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "set-model" {
			c.Fatalf("set-model present too early")
		}
		// make fake-reboot run after all tasks
		if tsk.Kind() != "fake-reboot-and-stall" {
			fakeRebootTask.WaitFor(tsk)
		}
	}
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	// set model was injected by prepare-remodeling
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "set-model" {
			setModelTask = tsk
			break
		}
	}
	c.Check(chg.IsReady(), Equals, false)
	c.Assert(chg.Err(), IsNil)
	// injected by fake restart
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	// 3 calls: promote tried system, old & new model, just the new model
	c.Check(resealKeyCalls, Equals, 3)
	// even if errors occur during reseal, set-model is done
	c.Check(setModelTask.Status(), Equals, state.DoneStatus)

	// reset the set-model state back to do, simulating a task restart after a reboot
	setModelTask.SetStatus(state.DoStatus)

	// the seeded systems has already been populated
	var seededSystems []devicestate.SeededSystem
	c.Assert(s.state.Get("seeded-systems", &seededSystems), IsNil)
	c.Assert(seededSystems, HasLen, 2)
	// we need to be smart about checking seed time, also verify
	// timestamps separately to avoid timezone problems
	newSeededTs := seededSystems[0].SeedTime
	// the system was seeded after our mocked 'now' or at the same
	// time if clock resolution is very low, but not before it
	c.Check(newSeededTs.Before(now), Equals, false)
	seededSystems[0].SeedTime = time.Time{}
	c.Check(seededSystems[1].SeedTime.Equal(oldSeededTs), Equals, true)
	seededSystems[1].SeedTime = time.Time{}
	expectedSeededSystems := []devicestate.SeededSystem{
		{
			System:    expectedLabel,
			Model:     new.Model(),
			BrandID:   new.BrandID(),
			Revision:  new.Revision(),
			Timestamp: new.Timestamp(),
		},
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			Revision:  model.Revision(),
		},
	}
	c.Check(seededSystems, DeepEquals, expectedSeededSystems)

	fakeRebootCallsReady = true
	// now redo the task again
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(resealKeyCalls, Equals, 6)
	c.Check(setModelTask.Status(), Equals, state.DoneStatus)

	c.Assert(s.state.Get("seeded-systems", &seededSystems), IsNil)
	c.Assert(seededSystems, HasLen, 2)
	// seed time should be unchanged
	c.Check(seededSystems[0].SeedTime.Equal(newSeededTs), Equals, true)
	seededSystems[0].SeedTime = time.Time{}
	c.Check(seededSystems[1].SeedTime.Equal(oldSeededTs), Equals, true)
	seededSystems[1].SeedTime = time.Time{}
	c.Check(seededSystems, DeepEquals, []devicestate.SeededSystem{
		{
			System:    expectedLabel,
			Model:     new.Model(),
			BrandID:   new.BrandID(),
			Revision:  new.Revision(),
			Timestamp: new.Timestamp(),
		},
		{
			System:    "0000",
			Model:     model.Model(),
			BrandID:   model.BrandID(),
			Timestamp: model.Timestamp(),
			Revision:  model.Revision(),
		},
	})
}

func (s *deviceMgrRemodelSuite) testRemodelTasksSelfContainedModelMissingDep(c *C, missingWhat []string, missingWhen string) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	// set a model assertion we remodel from
	current := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	snapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo-missing-deps",
			SnapID:   snaptest.AssertedSnapID("foo-missing-deps"),
			Revision: snap.R("123"),
		},
		Type: "app",
		Base: "core20",
	}
	if strutil.ListContains(missingWhat, "base") {
		snapsupTemplate.Base = "foo-base"
	}
	if strutil.ListContains(missingWhat, "content") {
		snapsupTemplate.Prereq = []string{"foo-content"}
	}

	fooYaml := `
name: foo-missing-base
version: 1
@MISSING@
`
	contentPlug := `
plugs:
  foo-content-data:
    content: foo-provided-content
    interface: content
    target: $SNAP/data-dir
    default-provider: foo-content
`
	missing := ""
	if strutil.ListContains(missingWhat, "base") {
		missing += "base: foo-base\n"
	} else {
		missing += "base: core20\n"
	}

	if strutil.ListContains(missingWhat, "content") {
		missing += contentPlug
	}
	fooYaml = strings.Replace(fooYaml, "@MISSING@", missing, 1)
	// the gadget needs to be mocked
	info := snaptest.MakeSnapFileAndDir(c, fooYaml, nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("foo-missing-deps"),
		Revision: snap.R(1),
		RealName: "foo-missing-deps",
	})

	if missingWhen != "install" {
		snapstate.Set(s.state, info.InstanceName(), &snapstate.SnapState{
			SnapType:        string(info.Type()),
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&info.SideInfo}),
			Current:         info.Revision,
			TrackingChannel: "latest/stable",
		})
	}

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		if missingWhen != "install" {
			c.Errorf("unexpected call to install for snap %q", name)
			return nil, fmt.Errorf("unexpected call")
		}
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		prqt.Add(info)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", snapsupTemplate)
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		if missingWhen == "install" {
			c.Errorf("unexpected call to update for snap %q", name)
			return nil, fmt.Errorf("unexpected call")
		}
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")
		c.Check(opts.Channel, Equals, "latest/stable")

		prqt.Add(info)

		var ts *state.TaskSet
		if missingWhen == "update" {
			tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
			tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
			tValidate.WaitFor(tDownload)
			// set snap-setup on a different task now
			tValidate.Set("snap-setup", snapsupTemplate)
			tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
			tUpdate.WaitFor(tValidate)
			ts = state.NewTaskSet(tDownload, tValidate, tUpdate)
			ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		} else {
			// switch-channel
			tSwitch := s.state.NewTask("fake-switch-channel", fmt.Sprintf("Switch snap %s channel to %s", name, opts.Channel))
			ts = state.NewTaskSet(tSwitch)
			// no edge
		}
		return ts, nil
	})
	defer restore()

	new := s.brands.Model("canonical", "pc-new-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "foo-missing-deps",
				"id":   snaptest.AssertedSnapID("foo-missing-deps"),
			},
		},
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})

	msg := `cannot remodel to model that is not self contained:`
	if strutil.ListContains(missingWhat, "base") {
		msg += `
  - cannot use snap "foo-missing-deps": base "foo-base" is missing`
	}

	if strutil.ListContains(missingWhat, "content") {
		msg += `
  - cannot use snap "foo-missing-deps": default provider "foo-content" or any alternative provider for content "foo-provided-content" is missing`
	}

	c.Assert(err, ErrorMatches, msg)
	c.Assert(tss, IsNil)
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingBaseInstall(c *C) {
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"base"}, "install")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingBaseUpdate(c *C) {
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"base"}, "update")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingBaseSwitchChannel(c *C) {
	// snap is installed in the system, the update is a simple switch
	// channel operation hence no revision change; the model doesn't mention
	// the snap's base
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"base"}, "switch-channel")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingBaseExisting(c *C) {
	// a snap already exists in the system, has no updates but its base
	// isn't mentioned in the model
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"base"}, "existing")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingContentInstall(c *C) {
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"content"}, "install")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingContentExisting(c *C) {
	// a snap already exists in the system, has no updates but its default
	// content provider isn't mentioned in the model
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"content"}, "existing")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingAllInstall(c *C) {
	s.testRemodelTasksSelfContainedModelMissingDep(c, []string{"content", "base"}, "install")
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSelfContainedModelMissingDepsOfMultipleSnaps(c *C) {
	// multiple new snaps that are missing their dependencies, some
	// dependencies are shared between those snaps
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	// set a model assertion we remodel from
	current := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	// bar is missing the base, and a shared content provider which is
	// missed by foo as well
	fooYaml := `
name: foo-missing-deps
version: 1
base: foo-base
plugs:
  foo-content-data:
    content: foo-provided-content
    interface: content
    target: $SNAP/data-dir
    default-provider: foo-content
`
	fooInfo := snaptest.MakeSnapFileAndDir(c, fooYaml, nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("foo-missing-deps"),
		Revision: snap.R(1),
		RealName: "foo-missing-deps",
	})

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		if name != "foo-missing-deps" {
			c.Errorf("unexpected call to install for snap %q", name)
			return nil, fmt.Errorf("unexpected call")
		}
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		prqt.Add(fooInfo)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		snapsupFoo := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "foo-missing-deps",
				SnapID:   snaptest.AssertedSnapID("foo-missing-deps"),
				Revision: snap.R("123"),
			},
			Type:   "app",
			Base:   "foo-base",
			Prereq: []string{"foo-content"},
		}
		tDownload.Set("snap-setup", snapsupFoo)
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// bar is missing the base, and a shared content provider which is
	// missed by foo as well
	barYaml := `
name: bar-missing-deps
version: 1
base: bar-base
plugs:
  bar-content-data:
    content: foo-provided-content
    interface: content
    target: $SNAP/data-dir
    default-provider: foo-content
`
	// the gadget needs to be mocked
	barInfo := snaptest.MakeSnapFileAndDir(c, barYaml, nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("bar-missing-deps"),
		Revision: snap.R(1),
		RealName: "bar-missing-deps",
	})
	snapstate.Set(s.state, barInfo.InstanceName(), &snapstate.SnapState{
		SnapType:        string(barInfo.Type()),
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&barInfo.SideInfo}),
		Current:         barInfo.Revision,
		TrackingChannel: "latest/stable",
	})

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		if name != "bar-missing-deps" {
			c.Errorf("unexpected call to update for snap %q", name)
			return nil, fmt.Errorf("unexpected call")
		}
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")
		c.Check(opts.Channel, Equals, "latest/stable")

		prqt.Add(barInfo)

		return state.NewTaskSet(), nil
	})
	defer restore()

	new := s.brands.Model("canonical", "pc-new-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "foo-missing-deps",
				"id":   snaptest.AssertedSnapID("foo-missing-deps"),
			},
			map[string]interface{}{
				"name": "bar-missing-deps",
				"id":   snaptest.AssertedSnapID("bar-missing-deps"),
			},
		},
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})
	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})

	msg := `cannot remodel to model that is not self contained:
  - cannot use snap "foo-missing-deps": base "foo-base" is missing
  - cannot use snap "foo-missing-deps": default provider "foo-content" or any alternative provider for content "foo-provided-content" is missing
  - cannot use snap "bar-missing-deps": base "bar-base" is missing
  - cannot use snap "bar-missing-deps": default provider "foo-content" or any alternative provider for content "foo-provided-content" is missing`

	c.Assert(err, ErrorMatches, msg)
	c.Assert(tss, IsNil)
}

type fakeSequenceStore struct {
	storetest.Store

	fn func(*asserts.AssertionType, []string, int, *auth.UserState) (asserts.Assertion, error)
}

func (f *fakeSequenceStore) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	return f.fn(assertType, sequenceKey, sequence, user)
}

func (s *deviceMgrSuite) TestRemodelUpdateFromValidationSetLatest(c *C) {
	const sequence = ""
	s.testRemodelUpdateFromValidationSet(c, sequence)
}

func (s *deviceMgrSuite) TestRemodelUpdateFromValidationSetSpecific(c *C) {
	const sequence = "1"
	s.testRemodelUpdateFromValidationSet(c, sequence)
}

func (s *deviceMgrSuite) testRemodelUpdateFromValidationSet(c *C, sequence string) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	essentialSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "pc",
			SnapID:   snaptest.AssertedSnapID("pc"),
			Revision: snap.R(2),
		},
		Type: "gadget",
	}

	nonEssentialSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "snap-1",
			SnapID:   snaptest.AssertedSnapID("snap-1"),
			Revision: snap.R(2),
		},
		Type: "app",
	}

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		switch name {
		case "snap-1", "pc":
		default:
			c.Fatalf("unexpected snap update: %s", name)
		}

		c.Check(opts.Revision, Equals, snap.R(2))

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)

		switch name {
		case "snap-1":
			tValidate.Set("snap-setup", nonEssentialSnapsupTemplate)
		case "pc":
			tValidate.Set("snap-setup", essentialSnapsupTemplate)
		}

		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "core20",
			Revision: snap.R(1),
			SnapID:   snaptest.AssertedSnapID("core20"),
		}}),
		Current:         snap.R(1),
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "pc-kernel",
			Revision: snap.R(1),
			SnapID:   snaptest.AssertedSnapID("pc-kernel"),
		}}),
		Current:         snap.R(1),
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "pc",
			Revision: snap.R(1),
			SnapID:   snaptest.AssertedSnapID("pc"),
		}}),
		Current:         snap.R(1),
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	snapstate.Set(s.state, "snap-1", &snapstate.SnapState{
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "snap-1",
			Revision: snap.R(1),
			SnapID:   snaptest.AssertedSnapID("snap-1"),
		}}),
		Current:         snap.R(1),
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	validationSetInModel := map[string]interface{}{
		"account-id": "canonical",
		"name":       "vset-1",
		"mode":       "enforce",
	}

	if sequence != "" {
		validationSetInModel["sequence"] = sequence
	}

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":    "amd64",
		"base":            "core20",
		"revision":        "1",
		"validation-sets": []interface{}{validationSetInModel},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-1",
				"id":       snaptest.AssertedSnapID("snap-1"),
				"revision": "2",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       snaptest.AssertedSnapID("pc"),
				"revision": "2",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{
		Remodeling:     true,
		DeviceModel:    newModel,
		OldDeviceModel: currentModel,
		CtxStore: &fakeSequenceStore{
			fn: func(aType *asserts.AssertionType, key []string, seq int, _ *auth.UserState) (asserts.Assertion, error) {
				c.Check(aType, Equals, asserts.ValidationSetType)
				c.Check(key, DeepEquals, []string{"16", "canonical", "vset-1"})

				if sequence == "" {
					c.Check(seq, Equals, 0)
				} else {
					n, err := strconv.Atoi(sequence)
					c.Assert(err, IsNil)
					c.Check(seq, Equals, n)
				}

				return vset, nil
			},
		},
	}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	// 2*snap update, create recovery system, set model
	c.Assert(tss, HasLen, 4)
}

func (s *deviceMgrSuite) TestRemodelInvalidEssentialFromValidationSet(c *C) {
	s.testRemodelInvalidFromValidationSet(c, "pc")
}

func (s *deviceMgrSuite) TestRemodelInvalidNonEssentialFromValidationSet(c *C) {
	s.testRemodelInvalidFromValidationSet(c, "snap-1")
}

func (s *deviceMgrSuite) testRemodelInvalidFromValidationSet(c *C, invalidSnap string) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     invalidSnap,
				"id":       snaptest.AssertedSnapID(invalidSnap),
				"presence": "invalid",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{
		Remodeling:     true,
		DeviceModel:    newModel,
		OldDeviceModel: currentModel,
		CtxStore: &fakeSequenceStore{
			fn: func(aType *asserts.AssertionType, key []string, sequence int, _ *auth.UserState) (asserts.Assertion, error) {
				c.Check(aType, Equals, asserts.ValidationSetType)
				c.Check(key, DeepEquals, []string{"16", "canonical", "vset-1"})
				c.Check(sequence, Equals, 0)
				return vset, nil
			},
		},
	}

	_, err = devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, fmt.Sprintf("snap presence is marked invalid by validation set: %s", invalidSnap))
}

func (s *deviceMgrSuite) TestOfflineRemodelPresentValidationSet(c *C) {
	const withValSet = true
	s.testOfflineRemodelValidationSet(c, withValSet)
}

func (s *deviceMgrSuite) TestOfflineRemodelMissingValidationSet(c *C) {
	const withValSet = false
	s.testOfflineRemodelValidationSet(c, withValSet)
}

func (s *deviceMgrSuite) testOfflineRemodelValidationSet(c *C, withValSet bool) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "snap-1",
				"id":              snaptest.AssertedSnapID("snap-1"),
				"type":            "app",
				"default-channel": "latest/stable",
			},
		},
	})

	if withValSet {
		vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
			"type":         "validation-set",
			"authority-id": "canonical",
			"series":       "16",
			"account-id":   "canonical",
			"name":         "vset-1",
			"sequence":     "1",
			"snaps": []interface{}{
				map[string]interface{}{
					"name":     "snap-1",
					"id":       snaptest.AssertedSnapID("snap-1"),
					"presence": "invalid",
				},
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}, nil, "")
		c.Assert(err, IsNil)

		err = assertstate.Add(s.state, vset)
		c.Assert(err, IsNil)
	}

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{
		Remodeling:     true,
		DeviceModel:    newModel,
		OldDeviceModel: currentModel,
		CtxStore: &fakeSequenceStore{
			fn: func(*asserts.AssertionType, []string, int, *auth.UserState) (asserts.Assertion, error) {
				c.Errorf("should not be called during an offline remodel")
				return nil, nil
			},
		},
	}

	// content doesn't really matter for this test, since we just use the
	// presence of local snaps to determine if this is an offline remodel
	sis := make([]*snap.SideInfo, 1)
	paths := make([]string, 1)
	sis[0], paths[0] = createLocalSnap(c, "pc", snaptest.AssertedSnapID("pc"), 1, "gadget", "", nil)

	_, err = devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", sis, paths, devicestate.RemodelOptions{
		Offline: true,
	})
	if !withValSet {
		c.Assert(err, ErrorMatches, "validation-set assertion not found")
	} else {
		c.Assert(err, ErrorMatches, "snap presence is marked invalid by validation set: snap-1")
	}
}

func (s *deviceMgrSuite) TestOfflineRemodelMissingSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc-new",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})

	snapstatetest.InstallEssentialSnaps(c, s.state, "core20", nil, nil)

	_, err = devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, ErrorMatches, `no snap file provided for "pc-new"`)
}

func (s *deviceMgrSuite) TestOfflineRemodelPreinstalledIncorrectRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       snaptest.AssertedSnapID("pc-kernel"),
				"presence": "required",
				"revision": "2",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, vset)
	c.Assert(err, IsNil)

	snapstatetest.InstallEssentialSnaps(c, s.state, "core20", nil, nil)

	_, err = devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, ErrorMatches, `installed snap "pc-kernel" does not match revision required to be used for offline remodel: 2 != 1`)
}

func (s *deviceMgrSuite) TestOfflineRemodelPreinstalledUseOldRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core22", nil, nil)

	snapstatetest.InstallSnap(c, s.state, "name: pc\nversion: 1\ntype: gadget\nbase: core22", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc"),
		Revision: snap.R(1),
		RealName: "pc",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, s.state, "name: core22\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core22"),
		Revision: snap.R(1),
		RealName: "core22",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true})

	// install kernel and base at newer revisions, but the validation set will
	// require the older revisions. this should result in the remodeling code
	// finding the older revisions, and calling UpdateWithDeviceContext to swap
	// to them.
	baseInfo := snapstatetest.InstallSnap(c, s.state, "name: core22\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("core22"),
		Revision: snap.R(2),
		RealName: "core22",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true, PreserveSequence: true})

	kernelInfo := snapstatetest.InstallSnap(c, s.state, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
		Revision: snap.R(2),
		RealName: "pc-kernel",
		Channel:  "latest/stable",
	}, snapstatetest.InstallSnapOptions{Required: true, PreserveSequence: true})

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(_ *state.State, name string, opts *snapstate.RevisionOptions, _ int, _ snapstate.Flags, prqt snapstate.PrereqTracker, _ snapstate.DeviceContext, _ string) (*state.TaskSet, error) {
		var info *snap.Info
		switch name {
		case "pc-kernel":
			info = kernelInfo
		case "core22":
			info = baseInfo
		default:
			c.Fatalf("unexpected snap update: %s", name)
		}

		c.Check(opts.Revision, Equals, snap.R(1))
		prqt.Add(baseInfo)

		prepare := s.state.NewTask("prepare-snap", fmt.Sprintf("prepare %s", name))
		prepare.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &info.SideInfo,
		})

		link := s.state.NewTask("link-snap", fmt.Sprintf("link %s", name))
		link.WaitFor(prepare)

		ts := state.NewTaskSet(prepare, link)
		ts.MarkEdge(prepare, snapstate.LastBeforeLocalModificationsEdge)

		return ts, nil
	})
	defer restore()

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core22",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core22",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       snaptest.AssertedSnapID("pc-kernel"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "core22",
				"id":       snaptest.AssertedSnapID("core22"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, vset)
	c.Assert(err, IsNil)

	chg, err := devicestate.Remodel(s.state, newModel, nil, nil, devicestate.RemodelOptions{
		Offline: true,
	})
	c.Assert(err, IsNil)

	// 2 for each snap (2), 2 to create the recovery system, 1 to set the model
	c.Assert(chg.Tasks(), HasLen, 2*2+2+1)

	kinds := make([]string, 0, len(chg.Tasks()))
	for _, t := range chg.Tasks() {
		kinds = append(kinds, t.Kind())
	}

	c.Check(kinds, DeepEquals, []string{
		"prepare-snap", "link-snap",
		"prepare-snap", "link-snap",
		"create-recovery-system", "finalize-recovery-system",
		"set-model",
	})
}

func (s *deviceMgrSuite) TestRemodelRequiredSnapMissingFromModel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	currentModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})
	err := assertstate.Add(s.state, currentModel)
	c.Assert(err, IsNil)

	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	c.Assert(err, IsNil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"revision":     "1",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "canonical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "latest/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "latest/stable",
			},
		},
	})

	vset, err := s.brands.Signing("canonical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-1",
				"id":       snaptest.AssertedSnapID("snap-1"),
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{
		Remodeling:     true,
		DeviceModel:    newModel,
		OldDeviceModel: currentModel,
		CtxStore: &fakeSequenceStore{
			fn: func(aType *asserts.AssertionType, key []string, sequence int, _ *auth.UserState) (asserts.Assertion, error) {
				c.Check(aType, Equals, asserts.ValidationSetType)
				c.Check(key, DeepEquals, []string{"16", "canonical", "vset-1"})
				c.Check(sequence, Equals, 0)
				return vset, nil
			},
		},
	}

	_, err = devicestate.RemodelTasks(context.Background(), s.state, currentModel, newModel, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, "missing required snap in model: snap-1")
}

func (s *deviceMgrRemodelSuite) TestRemodelVerifyOrderOfTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	// set a model assertion we remodel from
	current := s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	kernelSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "kernel-new",
			SnapID:   snaptest.AssertedSnapID("kernel-new"),
			Revision: snap.R("123"),
		},
		Type: "kernel",
	}

	fooSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo-with-base",
			SnapID:   snaptest.AssertedSnapID("foo-with-base"),
			Revision: snap.R("123"),
		},
		Type: "app",
		Base: "foo-base",
	}

	fooBaseSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo-base",
			SnapID:   snaptest.AssertedSnapID("foo-base"),
			Revision: snap.R("123"),
		},
		Type: "base",
	}

	barSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "bar-with-base",
			SnapID:   snaptest.AssertedSnapID("bar-with-base"),
			Revision: snap.R("123"),
		},
		Type: "app",
		Base: "bar-base",
	}

	barBaseSnapsupTemplate := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "bar-base",
			SnapID:   snaptest.AssertedSnapID("bar-base"),
			Revision: snap.R("123"),
		},
		Type: "base",
	}

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// currently we do not set essential snaps as required as they are
		// prevented from being removed by other means
		if name != "kernel-new" {
			c.Check(flags.Required, Equals, true)
		}
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		switch name {
		case "kernel-new":
			tDownload.Set("snap-setup", kernelSnapsupTemplate)
		case "foo-with-base":
			tDownload.Set("snap-setup", fooSnapsupTemplate)
		case "bar-with-base":
			tDownload.Set("snap-setup", barSnapsupTemplate)
		case "foo-base":
			tDownload.Set("snap-setup", fooBaseSnapsupTemplate)
		case "bar-base":
			tDownload.Set("snap-setup", barBaseSnapsupTemplate)
		default:
			c.Fatalf("unexpected call to install for snap %q", name)
		}
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	new := s.brands.Model("canonical", "pc-new-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "kernel-new",
				"id":              snaptest.AssertedSnapID("kernel-new"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "foo-with-base",
				"id":   snaptest.AssertedSnapID("foo-with-base"),
			},
			map[string]interface{}{
				"name": "bar-with-base",
				"id":   snaptest.AssertedSnapID("bar-with-base"),
			},
			map[string]interface{}{
				"name": "foo-base",
				"type": "base",
				"id":   snaptest.AssertedSnapID("foo-base"),
			},
			map[string]interface{}{
				"name": "bar-base",
				"type": "base",
				"id":   snaptest.AssertedSnapID("bar-base"),
			},
		},
	})

	// current gadget
	siModelGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   snaptest.AssertedSnapID("pc"),
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType:        "gadget",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelGadget}),
		Current:         siModelGadget.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})

	// current kernel
	siModelKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(32),
		SnapID:   snaptest.AssertedSnapID("pc-kernel"),
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType:        "kernel",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelKernel}),
		Current:         siModelKernel.Revision,
		Active:          true,
		TrackingChannel: "20/stable",
	})

	// and base
	siModelBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(31),
		SnapID:   snaptest.AssertedSnapID("core20"),
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType:        "base",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siModelBase}),
		Current:         siModelBase.Revision,
		Active:          true,
		TrackingChannel: "latest/stable",
	})

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true, DeviceModel: new, OldDeviceModel: current}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99", nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	// 5 snaps + create recovery system + set model
	c.Assert(tss, HasLen, 7)

	verifyOrderOfSnapSetups(c, tss, []snap.Type{
		snap.TypeKernel, snap.TypeBase, snap.TypeBase, snap.TypeApp, snap.TypeApp,
	})
}

func verifyOrderOfSnapSetups(c *C, tss []*state.TaskSet, expectedTypes []snap.Type) {
	foundTypes := make([]snap.Type, 0, len(expectedTypes))
	for _, ts := range tss {
		snapsup := snapSetupFromTaskSet(ts)
		if snapsup == nil {
			continue
		}
		foundTypes = append(foundTypes, snapsup.Type)
	}
	c.Check(foundTypes, DeepEquals, expectedTypes)
}

func snapSetupFromTaskSet(ts *state.TaskSet) *snapstate.SnapSetup {
	for _, t := range ts.Tasks() {
		snapsup, err := snapstate.TaskSnapSetup(t)
		if err != nil {
			continue
		}
		return snapsup
	}
	return nil
}

func (s *deviceMgrRemodelSuite) TestRemodelHybridSystemSkipSeed(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(_ *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, _ snapstate.PrereqTracker, _ snapstate.DeviceContext, _ string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"classic":      "true",
		"distribution": "ubuntu",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed-null
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1G
      - name: ubuntu-boot
        role: system-boot
        type: 83,F9E14625-EF3E-4200-AFEF-AEBD407460C4
        size: 1G
      - name: ubuntu-data
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 2G
`

	gadgetFiles := [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	}

	snapstatetest.InstallEssentialSnaps(c, s.state, "core20", gadgetFiles, nil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"classic":      "true",
		"distribution": "ubuntu",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "21/edge",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "21/stable",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "latest/edge",
			},
		},
	})

	chg, err := devicestate.Remodel(s.state, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	c.Check(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tasks := chg.Tasks()

	// 3 snaps (3 tasks for each) + set-model
	c.Check(tasks, HasLen, 3*3+1)

	taskKinds := make([]string, 0, len(tasks))
	for _, t := range tasks {
		taskKinds = append(taskKinds, t.Kind())
	}

	// note the lack of tasks for creating a recovery system, since this model
	// has a system-seed-null partition
	c.Check(taskKinds, DeepEquals, []string{
		"fake-download", "validate-snap", "fake-update",
		"fake-download", "validate-snap", "fake-update",
		"fake-download", "validate-snap", "fake-update",
		"set-model",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelHybridSystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateUpdateWithDeviceContext(func(_ *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, _ snapstate.PrereqTracker, _ snapstate.DeviceContext, _ string) (*state.TaskSet, error) {
		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s from track %s", name, opts.Channel))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"classic":      "true",
		"distribution": "ubuntu",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

	var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1G
      - name: ubuntu-data
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 2G
`

	gadgetFiles := [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	}

	snapstatetest.InstallEssentialSnaps(c, s.state, "core20", gadgetFiles, nil)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "1",
		"classic":      "true",
		"distribution": "ubuntu",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "21/edge",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "21/stable",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              snaptest.AssertedSnapID("core20"),
				"type":            "base",
				"default-channel": "latest/edge",
			},
		},
	})

	chg, err := devicestate.Remodel(s.state, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	c.Check(chg.Summary(), Equals, "Refresh model assertion from revision 0 to 1")

	tasks := chg.Tasks()

	// 3 snaps (3 tasks for each) + recovery system (2 taskss) + set-model
	c.Check(tasks, HasLen, 3*3+2+1)

	taskKinds := make([]string, 0, len(tasks))
	for _, t := range tasks {
		taskKinds = append(taskKinds, t.Kind())
	}

	c.Check(taskKinds, DeepEquals, []string{
		"fake-download", "validate-snap", "fake-update",
		"fake-download", "validate-snap", "fake-update",
		"fake-download", "validate-snap", "fake-update",
		"create-recovery-system", "finalize-recovery-system",
		"set-model",
	})
}
