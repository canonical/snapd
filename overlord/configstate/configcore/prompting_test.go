// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package configcore_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type promptingSuite struct {
	configcoreSuite

	repo *interfaces.Repository
}

var _ = Suite(&promptingSuite{})

func (s *promptingSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	// mock minimum set of features for apparmor prompting
	s.AddCleanup(apparmor.MockFeatures(
		[]string{"policy:permstable32:prompt"}, nil,
		[]string{"prompt"}, nil,
	))

	s.repo = interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		c.Assert(s.repo.AddInterface(iface), IsNil)
	}

	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, s.repo)
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingDaemonRestartNoPristine(c *C) {
	doRestartChan := make(chan bool, 1)
	restore := configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(st, Equals, s.state)
		c.Check(t, Equals, restart.RestartDaemon)
		c.Check(rebootInfo, IsNil)
		doRestartChan <- true
	})
	defer restore()

	s.mockSnapd(c)
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap", hasHandler: true})

	snap, confName := features.AppArmorPrompting.ConfigOption()

	for _, expectedRestart := range []bool{true, false} {
		s.state.Lock()
		rt := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		rt.Set(snap, confName, expectedRestart)
		s.state.Unlock()

		// Precondition check set values
		var value bool
		err := rt.GetPristine(snap, confName, &value)
		c.Check(config.IsNoOption(err), Equals, true)
		c.Check(value, Equals, false)
		err = rt.Get(snap, confName, &value)
		c.Check(err, IsNil)
		c.Check(value, Equals, expectedRestart)

		err = configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
		c.Check(err, IsNil)

		var observedRestart bool
		select {
		case <-doRestartChan:
			observedRestart = true
		default:
			observedRestart = false
		}
		c.Check(observedRestart, Equals, expectedRestart)
	}
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingDaemonRestartWithPristine(c *C) {
	doRestartChan := make(chan bool, 1)
	restore := configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(st, Equals, s.state)
		c.Check(t, Equals, restart.RestartDaemon)
		c.Check(rebootInfo, IsNil)
		doRestartChan <- true
	})
	defer restore()

	s.mockSnapd(c)
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap", hasHandler: true})

	snap, confName := features.AppArmorPrompting.ConfigOption()

	testCases := []struct {
		initial bool
		final   bool
	}{
		{
			false,
			false,
		},
		{
			false,
			true,
		},
		{
			true,
			false,
		},
		{
			true,
			true,
		},
	}
	for _, testCase := range testCases {
		s.state.Lock()
		rt := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		// Set value which will be read as pristine
		rt.Set(snap, confName, testCase.initial)
		rt.Commit()
		// Set value which will be read as current
		rt = configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		rt.Set(snap, confName, testCase.final)
		s.state.Unlock()

		// Precondition check set values
		var value bool
		err := rt.GetPristine(snap, confName, &value)
		c.Check(err, Equals, nil)
		c.Check(value, Equals, testCase.initial, Commentf("initial: %v, final: %v", testCase.initial, testCase.final))
		err = rt.Get(snap, confName, &value)
		c.Check(err, IsNil)
		c.Check(value, Equals, testCase.final, Commentf("initial: %v, final: %v", testCase.initial, testCase.final))

		err = configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
		c.Check(err, IsNil)

		expectedRestart := testCase.initial != testCase.final
		var observedRestart bool
		select {
		case <-doRestartChan:
			observedRestart = true
		default:
			observedRestart = false
		}
		c.Check(observedRestart, Equals, expectedRestart, Commentf("with initial value %v and final value %v, expected %v but observed %v", testCase.initial, testCase.final, expectedRestart, observedRestart))
	}
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingDaemonRestartErrors(c *C) {
	restore := configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()

	snap, confName := features.AppArmorPrompting.ConfigOption()

	// Check that failed Get returns an error
	s.state.Lock()
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, "invalid")
	s.state.Unlock()

	err := configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
	c.Check(err, Not(IsNil))

	// Check that failed GetPristine returns an error
	s.state.Lock()
	rt = configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, "invalid")
	rt.Commit()
	rt = configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, true)
	s.state.Unlock()

	err = configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
	c.Check(err, Not(IsNil))
}

func (s *promptingSuite) testDoExperimentalApparmorPromptingUnsupported(c *C, expectedError string) {
	snap, confName := features.AppArmorPrompting.ConfigOption()

	// one cannot enable prompting if it's not supported
	s.state.Lock()
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, true)
	s.state.Unlock()

	err := configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
	c.Check(err, ErrorMatches, expectedError)

	// but disabling it will not error out
	s.state.Lock()
	rt = configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, false)
	rt.Commit()
	s.state.Unlock()

	err = configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
	c.Check(err, IsNil)
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingUnsupportedKernel(c *C) {
	restore := apparmor.MockFeatures(
		[]string{"policy:permstable32:prompt-is-not-supported"}, nil,
		[]string{"prompt-is-not-supported"}, nil,
	)
	defer restore()

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()
	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature as it is not supported by the system: apparmor kernel features do not support prompting")
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingUnsupportedParser(c *C) {
	restore := apparmor.MockFeatures(
		[]string{"policy:permstable32:prompt"}, nil,
		[]string{"prompt-is-not-supported"}, nil,
	)
	defer restore()

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()
	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature as it is not supported by the system: apparmor parser does not support the prompt qualifier")
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingOnCoreUnsupported(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	release.MockOnCoreDesktop(false)
	defer restore()

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()

	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature as it is not supported on Ubuntu Core systems")
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingOnCoreDesktop(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	release.MockOnCoreDesktop(true)
	defer restore()

	s.mockSnapd(c)
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap", hasHandler: true})

	restartCalled := 0
	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		restartCalled++
	})
	defer restore()

	snap, confName := features.AppArmorPrompting.ConfigOption()

	// one cannot enable prompting if it's not supported
	s.state.Lock()
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	rt.Set(snap, confName, true)
	s.state.Unlock()

	err := configcore.DoExperimentalApparmorPromptingDaemonRestart(rt, nil)
	c.Check(err, IsNil)

	c.Check(restartCalled, Equals, 1)
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingChecksHandlersNone(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.mockSnapd(c)

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()

	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature no interfaces requests handlers are installed")
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingChecksHandlersManyButNoHandlerApp(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.mockSnapd(c)
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap1", hasHandler: false})
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap2", hasHandler: false})

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()

	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature no interfaces requests handlers are installed")
}

func (s *promptingSuite) TestDoExperimentalApparmorPromptingChecksHandlersDisconnected(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.mockSnapd(c)
	s.mockPromptingHandler(c, mockPromptingHandlerOpts{snapName: "test-snap", hasHandler: true})

	restore = configcore.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Errorf("unexpected restart requested")
	})
	defer restore()

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"test-snap:snap-interfaces-requests-control core:snap-interfaces-requests-control": map[string]interface{}{
			"interface": "snap-interfaces-requests-control",
			"plug-static": map[string]interface{}{
				"handler-service": "prompts-handler",
			},
			// manually disconnected
			"undesired": true,
		},
	})
	s.state.Unlock()

	s.testDoExperimentalApparmorPromptingUnsupported(c,
		"cannot enable prompting feature no interfaces requests handlers are installed")
}

func (s *promptingSuite) mockSnapd(c *C) {
	const snapdSnapYaml = `
name: snapd
version: 1
type: snapd
`

	si := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapdSnap := snaptest.MockSnap(c, snapdSnapYaml, si)
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "snapd",
	})

	for _, iface := range builtin.Interfaces() {
		if name := iface.Name(); name == "snap-interfaces-requests-control" {
			// add implicit slot
			// XXX copied from implicit.go
			snapdSnap.Slots[name] = &snap.SlotInfo{
				Name:      name,
				Snap:      snapdSnap,
				Interface: name,
			}
		}
	}
	snapdAppSet, err := interfaces.NewSnapAppSet(snapdSnap, nil)
	c.Assert(err, IsNil)
	c.Assert(s.repo.AddAppSet(snapdAppSet), IsNil)
}

type mockPromptingHandlerOpts struct {
	snapName   string
	hasHandler bool
}

func (s *promptingSuite) mockPromptingHandler(c *C, opts mockPromptingHandlerOpts) {
	name := opts.snapName

	var mockSnapWithPromptshandlerFmt = `name: %s
version: 1.0
apps:

plugs:
 snap-interfaces-requests-control:
`

	if opts.hasHandler {
		mockSnapWithPromptshandlerFmt = `name: %s
version: 1.0
apps:
 prompts-handler:
  daemon: simple

plugs:
 snap-interfaces-requests-control:
  handler: prompts-handler
`
	}
	si := &snap.SideInfo{RealName: name, Revision: snap.R(1)}
	snaptest.MockSnap(c, fmt.Sprintf(mockSnapWithPromptshandlerFmt, name), si)
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, name, &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	plugStatic := map[string]interface{}{}
	if opts.hasHandler {
		plugStatic["handler-service"] = "prompts-handler"
	}

	s.state.Set("conns", map[string]interface{}{
		fmt.Sprintf("%s:snap-interfaces-requests-control core:snap-interfaces-requests-control", name): map[string]interface{}{
			"interface":   "snap-interfaces-requests-control",
			"plug-static": plugStatic,
		},
	})
}
