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

package snapstate_test

import (
	"errors"
	"fmt"
	"os/user"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

type checkSnapSuite struct {
	testutil.BaseTest
	st        *state.State
	deviceCtx snapstate.DeviceContext
}

var _ = Suite(&checkSnapSuite{})

func (s *checkSnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.st = state.New(nil)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.deviceCtx = &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModel(map[string]interface{}{
		"kernel": "kernel",
		"gadget": "gadget",
	})}
}

func (s *checkSnapSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func emptyContainer(c *C) snap.Container {
	return snaptest.MockContainer(c, nil)
}

func (s *checkSnapSuite) TestCheckSnapErrorOnUnsupportedArchitecture(c *C) {
	const yaml = `name: hello
version: 1.10
architectures:
    - yadayada
    - blahblah
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil))

	errorMsg := fmt.Sprintf(`snap "hello" supported architectures (yadayada, blahblah) are incompatible with this system (%s)`, arch.DpkgArchitecture())
	c.Assert(err.Error(), Equals, errorMsg)
}

var assumesTests = []struct {
	version string
	assumes string
	classic bool
	error   string
}{
	{
		assumes: "[common-data-dir]",
	}, {
		assumes: "[f1, f2]",
		error:   `snap "foo" assumes unsupported features: f1, f2 \(try to refresh snapd\)`,
	}, {
		assumes: "[f1, f2]",
		classic: true,
		error:   `snap "foo" assumes unsupported features: f1, f2 \(try to refresh snapd\)`,
	}, {
		assumes: "[snapd2.15]",
		version: "unknown",
	}, {
		assumes: "[snapdnono]",
		version: "unknown",
		error:   `.* unsupported features: snapdnono .*`,
	}, {
		assumes: "[snapd2.15nono]",
		version: "unknown",
		error:   `.* unsupported features: snapd2.15nono .*`,
	}, {
		assumes: "[snapd2.15~pre1]",
		version: "unknown",
		error:   `.* unsupported features: snapd2.15~pre1 .*`,
	}, {
		assumes: "[snapd2.15]",
		version: "2.15",
	}, {
		assumes: "[snapd2.15]",
		version: "2.15.1",
	}, {
		assumes: "[snapd2.15]",
		version: "2.15+git",
	}, {
		assumes: "[snapd2.15]",
		version: "2.16",
	}, {
		assumes: "[snapd2.15.1]",
		version: "2.16",
	}, {
		assumes: "[snapd2.15.1]",
		version: "2.15.1",
	}, {
		assumes: "[snapd2.15.1.2]",
		version: "2.15.1.2",
	}, {
		assumes: "[snapd2.15.1.2]",
		version: "2.15.1.3",
	}, {
		// the horror the horror!
		assumes: "[snapd2.15.1.2.4.5.6.7.8.8]",
		version: "2.15.1.2.4.5.6.7.8.8",
	}, {
		assumes: "[snapd2.15.1.2.4.5.6.7.8.8]",
		version: "2.15.1.2.4.5.6.7.8.9",
	}, {
		assumes: "[snapd2.15.1.2]",
		version: "2.15.1.3",
	}, {
		assumes: "[snapd2.15.2]",
		version: "2.16.1",
	}, {
		assumes: "[snapd2.1000]",
		version: "3.1",
	}, {
		assumes: "[snapd3]",
		version: "3.1",
	}, {
		assumes: "[snapd2]",
		version: "3.1",
	}, {
		assumes: "[snapd3]",
		version: "2.48",
		error:   `.* unsupported features: snapd3 .*`,
	}, {
		assumes: "[snapd2.15.1.2]",
		version: "2.15.1.1",
		error:   `.* unsupported features: snapd2\.15\.1\.2 .*`,
	}, {
		assumes: "[snapd2.15.1.2.4.5.6.7.8.8]",
		version: "2.15.1.2.4.5.6.7.8.1",
		error:   `.* unsupported features: snapd2\.15\.1\.2\.4\.5\.6\.7\.8\.8 .*`,
	}, {
		assumes: "[snapd2.16]",
		version: "2.15",
		error:   `.* unsupported features: snapd2\.16 .*`,
	}, {
		assumes: "[snapd2.15.1]",
		version: "2.15",
		error:   `.* unsupported features: snapd2\.15\.1 .*`,
	}, {
		assumes: "[snapd2.15.1]",
		version: "2.15.0",
		error:   `.* unsupported features: snapd2\.15\.1 .*`,
	}, {
		// Note that this is different from how strconv.VersionCompare
		// (dpkg version numbering) would behave - it would error here
		assumes: "[snapd2.15]",
		version: "2.15~pre1",
	}, {
		assumes: "[command-chain]",
	}, {
		assumes: "[kernel-assets]",
	}, {
		assumes: "[snap-uid-envvars]",
	},
}

func (s *checkSnapSuite) TestCheckSnapAssumes(c *C) {
	restore := snapdtool.MockVersion("2.15")
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	for _, test := range assumesTests {

		snapdtool.Version = test.version
		if snapdtool.Version == "" {
			snapdtool.Version = "2.15"
		}

		comment := Commentf("snap assumes %q, but snapd version is %q", test.assumes, snapdtool.Version)
		release.OnClassic = test.classic

		yaml := fmt.Sprintf("name: foo\nversion: 1.0\nassumes: %s\n", test.assumes)

		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
		c.Assert(err, IsNil, comment)

		openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()
		mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
	}
}

func (s *checkSnapSuite) TestCheckSnapCheckCallbackOK(c *C) {
	const yaml = `name: foo
version: 1.0`

	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, yaml, si)
		cont := snaptest.MockContainer(c, [][]string{{"canary", "canary"}})
		return info, cont, nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	checkCbCalled := false
	checkCb := func(st *state.State, s, cur *snap.Info, sf snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		c.Assert(sf, NotNil)
		data := mylog.Check2(sf.ReadFile("canary"))

		c.Assert(data, DeepEquals, []byte("canary"))
		c.Assert(s.InstanceName(), Equals, "foo")
		c.Assert(s.SnapID, Equals, "snap-id")
		checkCbCalled = true
		return nil
	}
	r2 := snapstate.MockCheckSnapCallbacks([]snapstate.CheckSnapCallback{checkCb})
	defer r2()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", si, nil, snapstate.Flags{}, nil))
	c.Check(err, IsNil)

	c.Check(checkCbCalled, Equals, true)
}

func (s *checkSnapSuite) TestCheckSnapCheckCallbackFail(c *C) {
	const yaml = `name: foo
version: 1.0`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	fail := errors.New("bad snap")
	checkCb := func(st *state.State, s, cur *snap.Info, _ snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		return fail
	}
	r2 := snapstate.MockCheckSnapCallbacks(nil)
	defer r2()
	snapstate.AddCheckSnapCallback(checkCb)
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil))
	c.Check(err, Equals, fail)
}

func (s *checkSnapSuite) TestCheckSnapGadgetUpdate(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2), SnapID: "gadget-id"}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "gadget-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapGadgetUpdateLocal(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2)}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	// no SnapID => local!


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapGadgetUpdateToUnassertedProhibited(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2), SnapID: "gadget-id"}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, ErrorMatches, `cannot replace signed gadget snap with an unasserted one`)
}

func (s *checkSnapSuite) setupKernelGadgetSnaps(st *state.State) {
	gadgetInfo := &snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(1),
		SnapID:   "gadget-id",
	}
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: string(snap.TypeGadget),
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{gadgetInfo}),
		Current:  gadgetInfo.Revision,
	})

	kernelInfo := &snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(2),
		SnapID:   "kernel-id",
	}
	snapstate.Set(st, "kernel", &snapstate.SnapState{
		SnapType: string(snap.TypeKernel),
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{kernelInfo}),
		Current:  kernelInfo.Revision,
	})
}

func (s *checkSnapSuite) mockEssSnap(c *C, snapType string, snapId string) (restore func()) {
	const snapYaml = `name: %[1]s
type: %[1]s
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(fmt.Sprintf(snapYaml, snapType))))
	info.SnapID = snapId


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}

	return snapstate.MockOpenSnapFile(openSnapFile)
}

func (s *checkSnapSuite) TestCheckUnassertedOrAssertedGadgetKernelSnapVsModelGrade(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	gradeUnsetDeviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel(map[string]interface{}{
			"kernel": "kernel",
			"gadget": "gadget",
		}),
	}
	c.Check(gradeUnsetDeviceCtx.DeviceModel.Grade(), Equals, asserts.ModelGradeUnset)
	gradeSignedDeviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel20("gadget", map[string]interface{}{
			"base":  "core20",
			"grade": "signed",
		}),
	}
	c.Check(gradeSignedDeviceCtx.DeviceModel.Grade(), Equals, asserts.ModelSigned)
	gradeDangerousDeviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel20("gadget", map[string]interface{}{
			"base":  "core20",
			"grade": "dangerous",
		}),
	}
	c.Check(gradeDangerousDeviceCtx.DeviceModel.Grade(), Equals, asserts.ModelDangerous)

	tests := []struct {
		deviceCtx snapstate.DeviceContext
		essType   string
		snapID    string
		err       string
	}{
		{gradeUnsetDeviceCtx, "gadget", "gadget-id", ""},
		{gradeUnsetDeviceCtx, "kernel", "kernel-id", ""},
		{gradeSignedDeviceCtx, "gadget", "gadget-id", ""},
		{gradeSignedDeviceCtx, "kernel", "kernel-id", ""},
		{gradeDangerousDeviceCtx, "gadget", "gadget-id", ""},
		{gradeDangerousDeviceCtx, "kernel", "kernel-id", ""},
		{gradeUnsetDeviceCtx, "gadget", "", `cannot replace signed gadget snap with an unasserted one`},
		{gradeUnsetDeviceCtx, "kernel", "", `cannot replace signed kernel snap with an unasserted one`},
		{gradeSignedDeviceCtx, "gadget", "", `cannot replace signed gadget snap with an unasserted one`},
		{gradeSignedDeviceCtx, "kernel", "", `cannot replace signed kernel snap with an unasserted one`},
		// these combos  are now allowed
		{gradeDangerousDeviceCtx, "gadget", "", ""},
		{gradeDangerousDeviceCtx, "kernel", "", ""},
	}

	for _, t := range tests {
		func() {
			st := state.New(nil)
			st.Lock()
			defer st.Unlock()
			s.setupKernelGadgetSnaps(st)

			essRestore := s.mockEssSnap(c, t.essType, t.snapID)
			defer essRestore()

			st.Unlock()
			mylog.Check(snapstate.CheckSnap(st, "snap-path", t.essType, nil, nil, snapstate.Flags{}, t.deviceCtx))
			st.Lock()
			comm := Commentf("%s %s %s", t.deviceCtx.Model().Grade(), t.essType, t.snapID)
			if t.err == "" {
				c.Check(err, IsNil, comm)
			} else {
				c.Check(err, ErrorMatches, t.err, comm)
			}
		}()
	}
}

func (s *checkSnapSuite) TestCheckSnapGadgetAdditionProhibited(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2)}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: zgadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, ErrorMatches, "cannot replace gadget snap with a different one")
}

func (s *checkSnapSuite) TestCheckSnapGadgetAdditionProhibitedBySnapID(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2), SnapID: "gadget-id"}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: zgadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "zgadget-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, ErrorMatches, "cannot replace gadget snap with a different one")
}

func (s *checkSnapSuite) TestCheckSnapGadgetNoPrior(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.Set("seeded", true)

	const yaml = `name: gadget
type: gadget
version: 1
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapErrorOnDevModeDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: devmode
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil))

	c.Assert(err, ErrorMatches, ".* requires devmode or confinement override")
}

func (s *checkSnapSuite) TestCheckSnapErrorOnClassicDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: classic
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(true)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil))

	c.Assert(err, ErrorMatches, ".* requires classic confinement")
}

func (s *checkSnapSuite) TestCheckSnapErrorClassicOnCoreDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: classic
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{Classic: true}, nil))

	c.Assert(err, ErrorMatches, ".* requires classic confinement which is only available on classic systems")
}

func (s *checkSnapSuite) TestCheckSnapErrorClassicModeForStrictOrDevmode(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: strict
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{Classic: true}, nil))

	c.Assert(err, ErrorMatches, `snap "hello" is not a classic confined snap`)
}

func (s *checkSnapSuite) TestCheckSnapKernelUpdate(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "kernel", Revision: snap.R(2), SnapID: "kernel-id"}
	snaptest.MockSnap(c, `
name: kernel
type: kernel
version: 1
`, si)
	snapstate.Set(st, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: kernel
type: kernel
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "kernel-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "kernel", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapKernelAdditionProhibitedBySnapID(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "kernel", Revision: snap.R(2), SnapID: "kernel-id"}
	snaptest.MockSnap(c, `
name: kernel
type: kernel
version: 1
`, si)
	snapstate.Set(st, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: zkernel
type: kernel
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "zkernel-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "kernel", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, ErrorMatches, "cannot replace kernel snap with a different one")
}

func (s *checkSnapSuite) TestCheckSnapNoStateInfoInternalError(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "other-kernel", Revision: snap.R(2), SnapID: "kernel-id"}
	snaptest.MockSnap(c, `
name: other-kernel
type: kernel
version: 1
`, si)
	// we have a state information for snap of type kernel, but it's a
	// different snap
	snapstate.Set(st, "other-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: kernel
type: kernel
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "kernel-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "kernel", nil, nil, snapstate.Flags{}, s.deviceCtx))
	st.Lock()
	c.Check(err, ErrorMatches, "internal error: no state for kernel snap \"kernel\"")
}

func (s *checkSnapSuite) TestCheckSnapBasesErrorsIfMissing(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const yaml = `name: requires-base
version: 1
base: some-base
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "requires-base", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, ErrorMatches, "cannot find required base \"some-base\"")
}

func (s *checkSnapSuite) TestCheckSnapBasesNoneHappy(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const yaml = `name: use-base-none
version: 1
base: none
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "use-base-none", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapBasesHappy(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "some-base", Revision: snap.R(1), SnapID: "some-base-id"}
	snaptest.MockSnap(c, `
name: some-base
type: base
version: 1
`, si)
	snapstate.Set(st, "some-base", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: requires-base
version: 1
base: some-base
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "requires-base", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapInstanceName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "foo", Revision: snap.R(1), SnapID: "some-base-id"}
	info := snaptest.MockSnap(c, `
name: foo
version: 1
`, si)
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "foo_instance", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, IsNil)

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "bar_instance", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, ErrorMatches, `cannot install snap "foo" using instance name "bar_instance"`)

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "other-name", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, ErrorMatches, `cannot install snap "foo" using instance name "other-name"`)
}

func (s *checkSnapSuite) TestCheckSnapCheckCallInstanceKeySet(c *C) {
	const yaml = `name: foo
version: 1.0`

	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, yaml, si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	checkCbCalled := false
	checkCb := func(st *state.State, s, cur *snap.Info, sf snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		c.Assert(sf, NotNil)
		c.Assert(s.InstanceName(), Equals, "foo_instance")
		c.Assert(s.SnapName(), Equals, "foo")
		c.Assert(s.SnapID, Equals, "snap-id")
		checkCbCalled = true
		return nil
	}
	r2 := snapstate.MockCheckSnapCallbacks([]snapstate.CheckSnapCallback{checkCb})
	defer r2()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo_instance", si, nil, snapstate.Flags{}, nil))
	c.Check(err, IsNil)

	c.Check(checkCbCalled, Equals, true)
}

func (s *checkSnapSuite) TestCheckSnapCheckEpochLocal(c *C) {
	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: foo, version: 1.0, epoch: 13}", si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", si, &snap.Info{}, snapstate.Flags{}, nil))
	c.Check(err, ErrorMatches, `cannot refresh "foo" to local snap with epoch 13, because it can't read the current epoch of 0`)
}

func (s *checkSnapSuite) TestCheckSnapCheckEpochNonLocal(c *C) {
	si := &snap.SideInfo{
		SnapID:   "snap-id",
		Revision: snap.R(42),
	}

	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: foo, version: 1.0, epoch: 13}", si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", si, &snap.Info{}, snapstate.Flags{}, nil))
	c.Check(err, ErrorMatches, `cannot refresh "foo" to new revision 42 with epoch 13, because it can't read the current epoch of 0`)
}

func (s *checkSnapSuite) TestCheckSnapBasesCoreCanBeUsedAsCore16(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core", Revision: snap.R(1), SnapID: "core-id"}
	snaptest.MockSnap(c, `
name: core
type: os
version: 1
`, si)
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: requires-core16
version: 1
base: core16
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "requires-core16", nil, nil, snapstate.Flags{}, nil))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapdHappy(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	for _, t := range []struct {
		yaml   string
		errStr string
	}{
		{"name: snapd\nversion: 1\ntype: snapd", ""},
		{"name: some-snap\nversion: 1\ntype: snapd", `cannot install snap "some-snap" of type "snapd" with a name other than "snapd"`},
		{"name: snapd_instance\nversion: 1\ntype: snapd", `cannot install snap "snapd_instance" of type "snapd" with a name other than "snapd"`},
	} {
		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(t.yaml)))


		openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()

		st.Unlock()
		mylog.Check(snapstate.CheckSnap(st, "snap-path", "snapd", nil, nil, snapstate.Flags{}, nil))
		st.Lock()
		if t.errStr == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.errStr)
		}
	}
}

// Note, invalid usernames checked in snap/info_snap_yaml.go
var systemUsernamesTests = []struct {
	snapID      string
	sysIDs      string
	classic     bool
	noRangeUser bool
	noUser      bool
	scVer       string
	error       string
}{
	{
		sysIDs: "snap_daemon: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	}, {
		sysIDs: "snap_daemon:\n    scope: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	}, {
		sysIDs: "snap_microk8s: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		snapID: "some-uninteresting-snap-id",
		error:  `snap "foo" is not allowed to use the system user "snap_microk8s"`,
	}, {
		snapID: "EaXqgt1lyCaxKaQCU349mlodBkDCXRcg", // microk8s
		sysIDs: "snap_microk8s: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	}, {
		// missing snap ID, therefore installation allowed
		sysIDs: "snap_microk8s:\n    scope: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	}, {
		sysIDs: "snap_daemon:\n    scope: private",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		error:  `snap "foo" requires unsupported user scope "private" for this version of snapd`,
	}, {
		sysIDs: "snap_daemon:\n    scope: external",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		error:  `snap "foo" requires unsupported user scope "external" for this version of snapd`,
	}, {
		sysIDs: "snap_daemon:\n    scope: other",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		error:  `snap "foo" requires unsupported user scope "other"`,
	}, {
		sysIDs:  "snap_daemon: shared",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
	}, {
		sysIDs:  "snap_daemon:\n    scope: shared",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
	}, {
		sysIDs:  "snap_daemon:\n    scope: private",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
		error:   `snap "foo" requires unsupported user scope "private" for this version of snapd`,
	}, {
		sysIDs:  "snap_daemon:\n    scope: external",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
		error:   `snap "foo" requires unsupported user scope "external" for this version of snapd`,
	}, {
		sysIDs:  "snap_daemon:\n    scope: other",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
		error:   `snap "foo" requires unsupported user scope "other"`,
	}, {
		sysIDs: "snap_daemon: shared\n  allowed-not: shared",
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		error:  `snap "foo" requires unsupported system username "allowed-not"`,
	}, {
		sysIDs:  "allowed-not: shared\n  snap_daemon: shared",
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		classic: true,
		error:   `snap "foo" requires unsupported system username "allowed-not"`,
	}, {
		sysIDs: "snap_daemon: shared",
		noUser: true,
		scVer:  "dead 2.4.1 deadbeef bpf-actlog",
		error:  `cannot ensure users for snap "foo" required system username "snap_daemon": cannot add user/group "snap_daemon", group exists and user does not`,
	}, {
		sysIDs:  "snap_daemon: shared",
		classic: true,
		noUser:  true,
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		error:   `cannot ensure users for snap "foo" required system username "snap_daemon": cannot add user/group "snap_daemon", group exists and user does not`,
	}, {
		sysIDs: "snap_daemon: shared",
		scVer:  "dead 2.3.3 deadbeef bpf-actlog",
		error:  `snap "foo" system usernames require a snapd built against libseccomp >= 2.4`,
	}, {
		sysIDs:  "snap_daemon: shared",
		classic: true,
		scVer:   "dead 2.3.3 deadbeef bpf-actlog",
		error:   `snap "foo" system usernames require a snapd built against libseccomp >= 2.4`,
	}, {
		sysIDs: "snap_daemon: shared",
		scVer:  "dead 3.0.0 deadbeef bpf-actlog",
	}, {
		sysIDs:  "snap_daemon: shared",
		classic: true,
		scVer:   "dead 3.0.0 deadbeef bpf-actlog",
	}, {
		sysIDs: "snap_daemon: shared",
		scVer:  "dead 2.4.1 deadbeef -",
		error:  `snap "foo" system usernames require a snapd built against golang-seccomp >= 0.9.1`,
	}, {
		sysIDs:  "snap_daemon: shared",
		classic: true,
		scVer:   "dead 2.4.1 deadbeef -",
		error:   `snap "foo" system usernames require a snapd built against golang-seccomp >= 0.9.1`,
	}, {
		sysIDs:      "snap_daemon: shared",
		noRangeUser: true,
		scVer:       "dead 2.4.1 deadbeef bpf-actlog",
		error:       `cannot ensure users for snap "foo" required system username "snap_daemon": cannot add user/group "snapd-range-524288-root", group exists and user does not`,
	}, {
		sysIDs:      "snap_daemon: shared",
		classic:     true,
		noRangeUser: true,
		scVer:       "dead 2.4.1 deadbeef bpf-actlog",
		error:       `cannot ensure users for snap "foo" required system username "snap_daemon": cannot add user/group "snapd-range-524288-root", group exists and user does not`,
	}, {
		sysIDs:  "snap_daemon: shared\n  daemon: shared",
		classic: true,
		scVer:   "dead 2.4.1 deadbeef bpf-actlog",
		error:   `snap "foo" requires unsupported system username "daemon"`,
	},
}

func (s *checkSnapSuite) TestCheckSnapSystemUsernames(c *C) {
	for _, test := range systemUsernamesTests {
		restore := seccomp_compiler.MockCompilerVersionInfo(test.scVer)
		defer restore()

		restore = release.MockOnClassic(test.classic)
		defer restore()

		var osutilEnsureSnapUserGroupCalls int
		if test.noRangeUser {
			restore = snapstate.MockOsutilEnsureSnapUserGroup(func(name string, id uint32, extraUsers bool) error {
				return fmt.Errorf(`cannot add user/group "%s", group exists and user does not`, name)
			})
		} else if test.noUser {
			restore = snapstate.MockOsutilEnsureSnapUserGroup(func(name string, id uint32, extraUsers bool) error {
				if name == "snapd-range-524288-root" {
					return nil
				}
				return fmt.Errorf(`cannot add user/group "%s", group exists and user does not`, name)
			})
		} else {
			restore = snapstate.MockOsutilEnsureSnapUserGroup(func(name string, id uint32, extraUsers bool) error {
				osutilEnsureSnapUserGroupCalls++
				return nil
			})
		}
		defer restore()

		yaml := fmt.Sprintf("name: foo\nsystem-usernames:\n  %s\n", test.sysIDs)

		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))

		info.SnapID = test.snapID

		openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore = snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()
		mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
			c.Check(osutilEnsureSnapUserGroupCalls, Equals, 0)
		} else {

			// one call for the range user, one for the system user
			c.Check(osutilEnsureSnapUserGroupCalls, Equals, 2)
		}
	}
}

func (s *checkSnapSuite) TestCheckSnapSystemUsernamesCallsSnapDaemon(c *C) {
	const yaml = `name: foo
version: 1.0
system-usernames:
  snap_daemon: shared`

	s.testCheckSnapSystemUsernamesCallsCommon(c, "snap_daemon", "584788", yaml)
}

func (s *checkSnapSuite) TestCheckSnapSystemUsernamesCallsSnapMicrok8s(c *C) {
	const yaml = `name: microk8s
version: 1.0
system-usernames:
  snap_microk8s: shared`

	s.testCheckSnapSystemUsernamesCallsCommon(c, "snap_microk8s", "584789", yaml)
}

func (s *checkSnapSuite) testCheckSnapSystemUsernamesCallsCommon(c *C, expectedUser, expectedID, yaml string) {
	r := osutil.MockFindGid(func(groupname string) (uint64, error) {
		if groupname == expectedUser || groupname == "snapd-range-524288-root" {
			return 0, user.UnknownGroupError(groupname)
		}
		return 0, fmt.Errorf("unexpected call to FindGid for %s", groupname)
	})
	defer r()

	r = osutil.MockFindUid(func(username string) (uint64, error) {
		if username == expectedUser || username == "snapd-range-524288-root" {
			return 0, user.UnknownUserError(username)
		}
		return 0, fmt.Errorf("unexpected call to FindUid for %s", username)
	})
	defer r()

	falsePath := osutil.LookPathDefault("false", "/bin/false")
	for _, classic := range []bool{false, true} {
		restore := release.MockOnClassic(classic)
		defer restore()

		restore = seccomp_compiler.MockCompilerVersionInfo("dead 2.4.1 deadbeef bpf-actlog")
		defer restore()

		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))


		openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore = snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()

		mockGroupAdd := testutil.MockCommand(c, "groupadd", "")
		defer mockGroupAdd.Restore()

		mockUserAdd := testutil.MockCommand(c, "useradd", "")
		defer mockUserAdd.Restore()
		mylog.Check(snapstate.CheckSnap(s.st, "snap-path", info.SnapName(), nil, nil, snapstate.Flags{}, nil))

		if classic {
			c.Check(mockGroupAdd.Calls(), DeepEquals, [][]string{
				{"groupadd", "--system", "--gid", "524288", "snapd-range-524288-root"},
				{"groupadd", "--system", "--gid", expectedID, expectedUser},
			})
			c.Check(mockUserAdd.Calls(), DeepEquals, [][]string{
				{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", "524288", "--no-user-group", "--uid", "524288", "snapd-range-524288-root"},
				{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", expectedID, "--no-user-group", "--uid", expectedID, expectedUser},
			})
		} else {
			c.Check(mockGroupAdd.Calls(), DeepEquals, [][]string{
				{"groupadd", "--system", "--gid", "524288", "--extrausers", "snapd-range-524288-root"},
				{"groupadd", "--system", "--gid", expectedID, "--extrausers", expectedUser},
			})
			c.Check(mockUserAdd.Calls(), DeepEquals, [][]string{
				{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", "524288", "--no-user-group", "--uid", "524288", "--extrausers", "snapd-range-524288-root"},
				{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", expectedID, "--no-user-group", "--uid", expectedID, "--extrausers", expectedUser},
			})

		}
	}
}

func (s *checkSnapSuite) TestCheckSnapRemodelKernel(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "kernel", Revision: snap.R(2), SnapID: "kernel-id"}
	snaptest.MockSnap(c, `
name: kernel
type: kernel
version: 1
`, si)
	snapstate.Set(st, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: new-kernel
type: kernel
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "new-kernel-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	// happy case, the new-kernel matches the model
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		Remodeling: true,
		DeviceModel: MakeModel(map[string]interface{}{
			"kernel": "new-kernel",
			"gadget": "gadget",
		}),
	}

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "new-kernel", nil, nil, snapstate.Flags{}, deviceCtx))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapRemodelGadget(c *C) {
	reset := release.MockOnClassic(false)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "gadget", Revision: snap.R(2), SnapID: "gadget-id"}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: 1
`, si)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	const yaml = `name: new-gadget
type: gadget
version: 2
`

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yaml)))
	info.SnapID = "new-gadget-id"


	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	// happy case, the new-gadget matches the model but we do not
	// support this yet
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		Remodeling: true,
		DeviceModel: MakeModel(map[string]interface{}{
			"kernel": "kernel",
			"gadget": "new-gadget",
		}),
	}

	st.Unlock()
	mylog.Check(snapstate.CheckSnap(st, "snap-path", "new-gadget", nil, nil, snapstate.Flags{}, deviceCtx))
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckConfigureHooksHappy(c *C) {
	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: snap-with-default-configure, version: 1.0}", si)
		info.Hooks["default-configure"] = &snap.HookInfo{}
		info.Hooks["configure"] = &snap.HookInfo{}
		return info, emptyContainer(c), nil
	}

	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "snap-with-default-configure", nil, nil, snapstate.Flags{}, nil))
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckConfigureHooksUnHappy(c *C) {
	openSnapFile := func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: snap-with-default-configure, version: 1.0}", si)
		info.Hooks["default-configure"] = &snap.HookInfo{}
		return info, emptyContainer(c), nil
	}

	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()
	mylog.Check(snapstate.CheckSnap(s.st, "snap-path", "snap-with-default-configure", nil, nil, snapstate.Flags{}, nil))
	c.Check(err, ErrorMatches, `cannot specify "default-configure" hook without "configure" hook`)
}
