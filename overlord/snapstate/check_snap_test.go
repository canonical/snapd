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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
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

func (s *checkSnapSuite) TestCheckSnapErrorOnUnsupportedArchitecture(c *C) {
	const yaml = `name: hello
version: 1.10
architectures:
    - yadayada
    - blahblah
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil)

	errorMsg := fmt.Sprintf(`snap "hello" supported architectures (yadayada, blahblah) are incompatible with this system (%s)`, arch.UbuntuArchitecture())
	c.Assert(err.Error(), Equals, errorMsg)
}

var assumesTests = []struct {
	version string
	assumes string
	classic bool
	error   string
}{{
	assumes: "[common-data-dir]",
}, {
	assumes: "[f1, f2]",
	error:   `snap "foo" assumes unsupported features: f1, f2 \(try to refresh the core snap\)`,
}, {
	assumes: "[f1, f2]",
	classic: true,
	error:   `snap "foo" assumes unsupported features: f1, f2 \(try to update snapd and refresh the core snap\)`,
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
	assumes: "[snapd2.15.2]",
	version: "2.16.1",
}, {
	assumes: "[snapd3]",
	version: "3.1",
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
	assumes: "[command-chain]",
}}

func (s *checkSnapSuite) TestCheckSnapAssumes(c *C) {
	restore := cmd.MockVersion("2.15")
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	for _, test := range assumesTests {
		cmd.Version = test.version
		if cmd.Version == "" {
			cmd.Version = "2.15"
		}
		release.OnClassic = test.classic

		yaml := fmt.Sprintf("name: foo\nversion: 1.0\nassumes: %s\n", test.assumes)

		info, err := snap.InfoFromSnapYaml([]byte(yaml))
		c.Assert(err, IsNil)

		var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()
		err = snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (s *checkSnapSuite) TestCheckSnapCheckCallbackOK(c *C) {
	const yaml = `name: foo
version: 1.0`

	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, yaml, si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	checkCbCalled := false
	checkCb := func(st *state.State, s, cur *snap.Info, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		c.Assert(s.InstanceName(), Equals, "foo")
		c.Assert(s.SnapID, Equals, "snap-id")
		checkCbCalled = true
		return nil
	}
	r2 := snapstate.MockCheckSnapCallbacks([]snapstate.CheckSnapCallback{checkCb})
	defer r2()

	err := snapstate.CheckSnap(s.st, "snap-path", "foo", si, nil, snapstate.Flags{}, nil)
	c.Check(err, IsNil)

	c.Check(checkCbCalled, Equals, true)
}

func (s *checkSnapSuite) TestCheckSnapCheckCallbackFail(c *C) {
	const yaml = `name: foo
version: 1.0`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	fail := errors.New("bad snap")
	checkCb := func(st *state.State, s, cur *snap.Info, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		return fail
	}
	r2 := snapstate.MockCheckSnapCallbacks(nil)
	defer r2()
	snapstate.AddCheckSnapCallback(checkCb)

	err = snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	info.SnapID = "gadget-id"
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	// no SnapID => local!
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: gadget
type: gadget
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx)
	st.Lock()
	c.Check(err, ErrorMatches, `cannot replace signed gadget snap with an unasserted one`)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: zgadget
type: gadget
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: zgadget
type: gadget
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	info.SnapID = "zgadget-id"
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, s.deviceCtx)
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
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "gadget", nil, nil, snapstate.Flags{}, nil)
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapErrorOnDevModeDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: devmode
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil)

	c.Assert(err, ErrorMatches, ".* requires devmode or confinement override")
}

func (s *checkSnapSuite) TestCheckSnapErrorOnClassicDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: classic
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(true)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{}, nil)

	c.Assert(err, ErrorMatches, ".* requires classic confinement")
}

func (s *checkSnapSuite) TestCheckSnapErrorClassicOnCoreDisallowed(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: classic
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{Classic: true}, nil)

	c.Assert(err, ErrorMatches, ".* requires classic confinement which is only available on classic systems")
}

func (s *checkSnapSuite) TestCheckSnapErrorClassicModeForStrictOrDevmode(c *C) {
	const yaml = `name: hello
version: 1.10
confinement: strict
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", "hello", nil, nil, snapstate.Flags{Classic: true}, nil)

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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: kernel
type: kernel
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	info.SnapID = "kernel-id"
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "kernel", nil, nil, snapstate.Flags{}, s.deviceCtx)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: zkernel
type: kernel
version: 2
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	info.SnapID = "zkernel-id"
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "kernel", nil, nil, snapstate.Flags{}, s.deviceCtx)
	st.Lock()
	c.Check(err, ErrorMatches, "cannot replace kernel snap with a different one")
}

func (s *checkSnapSuite) TestCheckSnapBasesErrorsIfMissing(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const yaml = `name: requires-base
version: 1
base: some-base
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "requires-base", nil, nil, snapstate.Flags{}, nil)
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
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "use-base-none", nil, nil, snapstate.Flags{}, nil)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: requires-base
version: 1
base: some-base
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "requires-base", nil, nil, snapstate.Flags{}, nil)
	st.Lock()
	c.Check(err, IsNil)
}

// emptyContainer returns a minimal container that passes
// ValidateContainer: / and /meta exist and are 0755, and
// /meta/snap.yaml is a regular world-readable file.
func emptyContainer(c *C) *snapdir.SnapDir {
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0444), IsNil)
	return snapdir.New(d)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err := snapstate.CheckSnap(st, "snap-path", "foo_instance", nil, nil, snapstate.Flags{}, nil)
	st.Lock()
	c.Check(err, IsNil)

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "bar_instance", nil, nil, snapstate.Flags{}, nil)
	st.Lock()
	c.Check(err, ErrorMatches, `cannot install snap "foo" using instance name "bar_instance"`)

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "other-name", nil, nil, snapstate.Flags{}, nil)
	st.Lock()
	c.Check(err, ErrorMatches, `cannot install snap "foo" using instance name "other-name"`)
}

func (s *checkSnapSuite) TestCheckSnapCheckCallInstanceKeySet(c *C) {
	const yaml = `name: foo
version: 1.0`

	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, yaml, si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	checkCbCalled := false
	checkCb := func(st *state.State, s, cur *snap.Info, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
		c.Assert(s.InstanceName(), Equals, "foo_instance")
		c.Assert(s.SnapName(), Equals, "foo")
		c.Assert(s.SnapID, Equals, "snap-id")
		checkCbCalled = true
		return nil
	}
	r2 := snapstate.MockCheckSnapCallbacks([]snapstate.CheckSnapCallback{checkCb})
	defer r2()

	err := snapstate.CheckSnap(s.st, "snap-path", "foo_instance", si, nil, snapstate.Flags{}, nil)
	c.Check(err, IsNil)

	c.Check(checkCbCalled, Equals, true)
}

func (s *checkSnapSuite) TestCheckSnapCheckEpochLocal(c *C) {
	si := &snap.SideInfo{
		SnapID: "snap-id",
	}

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: foo, version: 1.0, epoch: 13}", si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	err := snapstate.CheckSnap(s.st, "snap-path", "foo", si, &snap.Info{}, snapstate.Flags{}, nil)
	c.Check(err, ErrorMatches, `cannot refresh "foo" to local snap with epoch 13, because it can't read the current epoch of 0`)
}

func (s *checkSnapSuite) TestCheckSnapCheckEpochNonLocal(c *C) {
	si := &snap.SideInfo{
		SnapID:   "snap-id",
		Revision: snap.R(42),
	}

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		info := snaptest.MockInfo(c, "{name: foo, version: 1.0, epoch: 13}", si)
		return info, emptyContainer(c), nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	err := snapstate.CheckSnap(s.st, "snap-path", "foo", si, &snap.Info{}, snapstate.Flags{}, nil)
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
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	const yaml = `name: requires-core16
version: 1
base: core16
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", "requires-core16", nil, nil, snapstate.Flags{}, nil)
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
		info, err := snap.InfoFromSnapYaml([]byte(t.yaml))
		c.Assert(err, IsNil)

		var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()

		st.Unlock()
		err = snapstate.CheckSnap(st, "snap-path", "snapd", nil, nil, snapstate.Flags{}, nil)
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
	sysIDs  string
	classic bool
	noGroup bool
	noUser  bool
	scVer   string
	error   string
}{{
	sysIDs: "snap_daemon: shared",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
}, {
	sysIDs: "snap_daemon:\n    scope: shared",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
}, {
	sysIDs: "snap_daemon:\n    scope: private",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	error:  `Unsupported user scope "private" for this version of snapd`,
}, {
	sysIDs: "snap_daemon:\n    scope: external",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	error:  `Unsupported user scope "external" for this version of snapd`,
}, {
	sysIDs: "snap_daemon:\n    scope: other",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	error:  `Unsupported user scope "other"`,
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
	error:   `Unsupported user scope "private" for this version of snapd`,
}, {
	sysIDs:  "snap_daemon:\n    scope: external",
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	classic: true,
	error:   `Unsupported user scope "external" for this version of snapd`,
}, {
	sysIDs:  "snap_daemon:\n    scope: other",
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	classic: true,
	error:   `Unsupported user scope "other"`,
}, {
	sysIDs: "snap_daemon: shared\n  allowed-not: shared",
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	error:  `Unsupported system username "allowed-not"`,
}, {
	sysIDs:  "allowed-not: shared\n  snap_daemon: shared",
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	classic: true,
	error:   `Unsupported system username "allowed-not"`,
}, {
	sysIDs:  "snap_daemon: shared",
	noGroup: true,
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	error:   `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs:  "snap_daemon: shared",
	classic: true,
	noGroup: true,
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	error:   `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs: "snap_daemon: shared",
	noUser: true,
	scVer:  "dead 2.4.1 deadbeef bpf-actlog",
	error:  `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs:  "snap_daemon: shared",
	classic: true,
	noUser:  true,
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	error:   `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs:  "snap_daemon: shared",
	noUser:  true,
	noGroup: true,
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	error:   `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs:  "snap_daemon: shared",
	classic: true,
	noUser:  true,
	noGroup: true,
	scVer:   "dead 2.4.1 deadbeef bpf-actlog",
	error:   `This snap requires that both the \"snap_daemon\" system user and group are present on the system.`,
}, {
	sysIDs: "snap_daemon: shared",
	scVer:  "dead 2.3.3 deadbeef bpf-actlog",
	error:  `This snap requires that snapd be compiled against libseccomp >= 2.4.`,
}, {
	sysIDs:  "snap_daemon: shared",
	classic: true,
	scVer:   "dead 2.3.3 deadbeef bpf-actlog",
	error:   `This snap requires that snapd be compiled against libseccomp >= 2.4.`,
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
	error:  `This snap requires that snapd be compiled against golang-seccomp >= 0.9.1.`,
}, {
	sysIDs:  "snap_daemon: shared",
	classic: true,
	scVer:   "dead 2.4.1 deadbeef -",
	error:   `This snap requires that snapd be compiled against golang-seccomp >= 0.9.1.`,
}}

func (s *checkSnapSuite) TestCheckSnapSystemUsernames(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, test := range systemUsernamesTests {
		restore = interfaces.MockSeccompCompilerVersionInfo(func(_ string) (string, error) {
			return test.scVer, nil
		})
		defer restore()

		release.OnClassic = test.classic
		if test.noGroup {
			restore = snapstate.MockFindGid(func(name string) (uint64, error) {
				return 0, fmt.Errorf("user: unknown group %s", name)
			})
		} else {
			restore = snapstate.MockFindGid(func(name string) (uint64, error) {
				return 123, nil
			})
		}
		defer restore()

		if test.noUser {
			restore = snapstate.MockFindUid(func(name string) (uint64, error) {
				return 0, fmt.Errorf("user: unknown user %s", name)
			})
		} else {
			restore = snapstate.MockFindUid(func(name string) (uint64, error) {
				return 124, nil
			})
		}
		defer restore()

		yaml := fmt.Sprintf("name: foo\nsystem-usernames:\n  %s\n", test.sysIDs)

		info, err := snap.InfoFromSnapYaml([]byte(yaml))
		c.Assert(err, IsNil)

		var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
			return info, emptyContainer(c), nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()
		err = snapstate.CheckSnap(s.st, "snap-path", "foo", nil, nil, snapstate.Flags{}, nil)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Assert(err, IsNil)
		}
	}
}
