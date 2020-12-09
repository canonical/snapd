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

package devicestate_test

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
)

type deviceMgrRemodelSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrRemodelSuite{})

func (s *deviceMgrRemodelSuite) TestRemodelUnhappyNotSeeded(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", false)

	newModel := s.brands.Model("canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	_, err := devicestate.Remodel(s.state, newModel)
	c.Assert(err, ErrorMatches, "cannot remodel until fully seeded")
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: cur["brand"].(string),
		Model: cur["model"].(string),
	})

	// ensure all error cases are checked
	for _, t := range []struct {
		new    map[string]interface{}
		errStr string
	}{
		{map[string]interface{}{"architecture": "pdp-7"}, "cannot remodel to different architectures yet"},
		{map[string]interface{}{"base": "core18"}, "cannot remodel from core to bases yet"},
		{map[string]interface{}{"base": "core20", "kernel": nil, "gadget": nil, "snaps": mockCore20ModelSnaps}, "cannot remodel to Ubuntu Core 20 models yet"},
	} {
		mergeMockModelHeaders(cur, t.new)
		new := s.brands.Model(t.new["brand"].(string), t.new["model"].(string), t.new)
		chg, err := devicestate.Remodel(s.state, new)
		c.Check(chg, IsNil)
		c.Check(err, ErrorMatches, t.errStr)
	}
}

func (s *deviceMgrRemodelSuite) TestRemodelNoUC20RemodelYet(c *C) {
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: cur["brand"].(string),
		Model: cur["model"].(string),
	})

	// ensure all error cases are checked
	for _, t := range []struct {
		new    map[string]interface{}
		errStr string
	}{
		// uc20 model
		{map[string]interface{}{"grade": "signed"}, "cannot remodel Ubuntu Core 20 models yet"},
		{map[string]interface{}{"base": "core22"}, "cannot remodel Ubuntu Core 20 models yet"},
		// non-uc20 model
		{map[string]interface{}{"snaps": nil, "grade": nil, "base": "core", "gadget": "pc", "kernel": "pc-kernel"}, "cannot remodel Ubuntu Core 20 models yet"},
	} {
		mergeMockModelHeaders(cur, t.new)
		new := s.brands.Model(t.new["brand"].(string), t.new["model"].(string), t.new)
		chg, err := devicestate.Remodel(s.state, new)
		c.Check(chg, IsNil)
		c.Check(err, ErrorMatches, t.errStr)
	}
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

	var testDeviceCtx snapstate.DeviceContext

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, Equals, testDeviceCtx)
		c.Check(fromChange, Equals, "99")
		c.Check(name, Equals, whatRefreshes)
		c.Check(opts.Channel, Equals, "18")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99")
	c.Assert(err, IsNil)
	// 2 snaps, plus one track switch plus the remodel task, the
	// wait chain is tested in TestRemodel*
	c.Assert(tss, HasLen, 4)
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchGadget(c *C) {
	s.testRemodelSwitchTasks(c, "other-gadget", "18", map[string]interface{}{
		"gadget": "other-gadget=18",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelTasksSwitchKernel(c *C) {
	s.testRemodelSwitchTasks(c, "other-kernel", "18", map[string]interface{}{
		"kernel": "other-kernel=18",
	})
}

func (s *deviceMgrRemodelSuite) testRemodelSwitchTasks(c *C, whatsNew, whatNewTrack string, newModelOverrides map[string]interface{}) {
	c.Check(newModelOverrides, HasLen, 1, Commentf("test expects a single model property to change"))
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	var snapstateInstallWithDeviceContextCalled int
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		snapstateInstallWithDeviceContextCalled++
		c.Check(name, Equals, whatsNew)
		if whatNewTrack != "" {
			c.Check(opts.Channel, Equals, whatNewTrack)
		}

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99")
	c.Assert(err, IsNil)
	// 1 of switch-kernel/base/gadget plus the remodel task
	c.Assert(tss, HasLen, 2)
	// API was hit
	c.Assert(snapstateInstallWithDeviceContextCalled, Equals, 1)
}

func (s *deviceMgrRemodelSuite) TestRemodelRequiredSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
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

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()

	restore = devicestate.MockSnapstateUpdateWithDeviceContext(func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, false)
		c.Check(flags.NoReRefresh, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s to track %s", name, opts.Channel))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tUpdate := s.state.NewTask("fake-update", fmt.Sprintf("Update %s to track %s", name, opts.Channel))
		tUpdate.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tUpdate)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1"},
		"revision":       "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
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
	c.Assert(tDownloadKernel.Summary(), Equals, "Download pc-kernel to track 18")
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

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"some-required-snap"},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
		"revision":     "1",
	})
	chg, err := devicestate.Remodel(s.state, new)
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

func (sto *freshSessionStore) EnsureDeviceSession() (*auth.DeviceState, error) {
	sto.ensureDeviceSession += 1
	return nil, nil
}

func (s *deviceMgrRemodelSuite) TestRemodelStoreSwitch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testStore snapstate.StoreService

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		c.Check(deviceCtx.Store(), Equals, testStore)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
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

	chg, err := devicestate.Remodel(s.state, new)
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

	chg, err := devicestate.Remodel(s.state, new)
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

func (s *deviceMgrRemodelSuite) TestRemodelClash(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var clashing *asserts.Model

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate things changing under our feet
		assertstatetest.AddMany(st, clashing)
		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand: "canonical",
			Model: clashing.Model(),
		})

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

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
	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model-other (0)",
	})

	// reset
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	clashing = new
	_, err = devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent remodel to canonical/pc-model (1)",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodelClashInProgress(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		// simulate another started remodeling
		st.NewChange("remodel", "other remodel")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	new := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
		"revision":       "1",
	})

	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start remodel, clashing with concurrent one",
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

	// simulate any other change
	s.state.NewChange("chg", "other change")

	_, err := devicestate.Remodel(s.state, new)
	c.Check(err, DeepEquals, &snapstate.ChangeConflictError{
		Message: "cannot start complete remodel, other changes are in progress",
	})
}

func (s *deviceMgrRemodelSuite) TestRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no changes
	c.Check(devicestate.Remodeling(s.state), Equals, false)

	// other change
	s.state.NewChange("other", "...")
	c.Check(devicestate.Remodeling(s.state), Equals, false)

	// remodel change
	chg := s.state.NewChange("remodel", "...")
	c.Check(devicestate.Remodeling(s.state), Equals, true)

	// done
	chg.SetStatus(state.DoneStatus)
	c.Check(devicestate.Remodeling(s.state), Equals, false)
}

func (s *deviceMgrRemodelSuite) TestDeviceCtxNoTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// nothing in the state

	_, err := devicestate.DeviceCtx(s.state, nil, nil)
	c.Check(err, Equals, state.ErrNoState)

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

	s.setupBrands(c)

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
	err = ioutil.WriteFile(filepath.Join(info.MountDir(), "meta/gadget.yaml"), []byte(mockGadget), 0644)
	c.Assert(err, IsNil)

	err = devicestate.CheckGadgetRemodelCompatible(s.state, info, currInfo, snapf, snapstate.Flags{}, remodelCtx)
	c.Check(err, ErrorMatches, "cannot read current gadget metadata: .*/gadget/123/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml to the current gadget
	err = ioutil.WriteFile(filepath.Join(currInfo.MountDir(), "meta/gadget.yaml"), []byte(mockGadget), 0644)
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
		Sequence: []*snap.SideInfo{siCurrent},
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

	s.setupBrands(c)
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

	errMatch := `cannot remodel to an incompatible gadget: incompatible layout change: incompatible structure #0 \("foo"\) change: cannot change structure size from 10485760 to 20971520`
	s.testCheckGadgetRemodelCompatibleWithYaml(c, compatibleTestMockOkGadget, mockBadGadgetYaml, errMatch)
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

	nopHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}
	s.o.TaskRunner().AddHandler("fake-download", nopHandler, nil)
	s.o.TaskRunner().AddHandler("validate-snap", nopHandler, nil)
	s.o.TaskRunner().AddHandler("set-model", nopHandler, nil)

	// set a model assertion we remodel from
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

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
		Sequence: []*snap.SideInfo{siModelGadget},
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

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
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
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	gadgetUpdateCalled := false
	restore = devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		gadgetUpdateCalled = true
		c.Check(policy, NotNil)
		c.Check(reflect.ValueOf(policy).Pointer(), Equals, reflect.ValueOf(gadget.RemodelUpdatePolicy).Pointer())
		c.Check(current, DeepEquals, gadget.GadgetData{
			Info: &gadget.Info{
				Volumes: map[string]*gadget.Volume{
					"pc": {
						Bootloader: "grub",
						Schema:     "gpt",
						Structure: []gadget.VolumeStructure{{
							Name:       "foo",
							Type:       "00000000-0000-0000-0000-0000deadcafe",
							Size:       10 * quantity.SizeMiB,
							Filesystem: "ext4",
							Content: []gadget.VolumeContent{
								{UnresolvedSource: "foo-content", Target: "/"},
							},
						}, {
							Name: "bare-one",
							Type: "bare",
							Size: quantity.SizeMiB,
							Content: []gadget.VolumeContent{
								{Image: "bare.img"},
							},
						}},
					},
				},
			},
			RootDir: currentGadgetInfo.MountDir(),
		})
		c.Check(update, DeepEquals, gadget.GadgetData{
			Info: &gadget.Info{
				Volumes: map[string]*gadget.Volume{
					"pc": {
						Bootloader: "grub",
						Schema:     "gpt",
						Structure: []gadget.VolumeStructure{{
							Name:       "foo",
							Type:       "00000000-0000-0000-0000-0000deadcafe",
							Size:       10 * quantity.SizeMiB,
							Filesystem: "ext4",
							Content: []gadget.VolumeContent{
								{UnresolvedSource: "new-foo-content", Target: "/"},
							},
						}, {
							Name: "bare-one",
							Type: "bare",
							Size: quantity.SizeMiB,
							Content: []gadget.VolumeContent{
								{Image: "new-bare-content.img"},
							},
						}},
					},
				},
			},
			RootDir: newGadgetInfo.MountDir(),
		})
		return nil
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new)
	c.Check(err, IsNil)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(gadgetUpdateCalled, Equals, true)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrRemodelSuite) TestRemodelGadgetAssetsParanoidCheck(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	nopHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}
	s.o.TaskRunner().AddHandler("fake-download", nopHandler, nil)
	s.o.TaskRunner().AddHandler("validate-snap", nopHandler, nil)
	s.o.TaskRunner().AddHandler("set-model", nopHandler, nil)

	// set a model assertion we remodel from
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})

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
		Sequence: []*snap.SideInfo{siModelGadget},
		Current:  siModelGadget.Revision,
		Active:   true,
	})

	// new gadget snap, name does not match the new model
	siUnexpectedModelGadget := &snap.SideInfo{
		RealName: "new-gadget-unexpected",
		Revision: snap.R(34),
	}
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
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
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
		return ts, nil
	})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	gadgetUpdateCalled := false
	restore = devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()

	chg, err := devicestate.Remodel(s.state, new)
	c.Check(err, IsNil)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), ErrorMatches, `(?s).*\(cannot apply gadget assets update from non-model gadget snap "new-gadget-unexpected", expected "new-gadget" snap\)`)
	c.Check(gadgetUpdateCalled, Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSuite) TestRemodelSwitchBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	var testDeviceCtx snapstate.DeviceContext

	var snapstateInstallWithDeviceContextCalled int
	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		snapstateInstallWithDeviceContextCalled++
		c.Check(name, Equals, "core20")

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.DownloadAndChecksDoneEdge)
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

	testDeviceCtx = &snapstatetest.TrivialDeviceContext{Remodeling: true}

	tss, err := devicestate.RemodelTasks(context.Background(), s.state, current, new, testDeviceCtx, "99")
	c.Assert(err, IsNil)
	// 1 switch to a new base plus the remodel task
	c.Assert(tss, HasLen, 2)
	// API was hit
	c.Assert(snapstateInstallWithDeviceContextCalled, Equals, 1)
}
