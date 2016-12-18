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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	"github.com/snapcore/snapd/overlord/snapstate"
)

type checkSnapSuite struct {
	st *state.State
}

var _ = Suite(&checkSnapSuite{})

func (s *checkSnapSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.st = state.New(nil)
}

func (s *checkSnapSuite) TearDownTest(c *C) {
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})

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
			return info, nil, nil
		}
		restore := snapstate.MockOpenSnapFile(openSnapFile)
		defer restore()
		err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})
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
		return info, nil, nil
	}
	r1 := snapstate.MockOpenSnapFile(openSnapFile)
	defer r1()

	checkCbCalled := false
	checkCb := func(st *state.State, s, cur *snap.Info, flags snapstate.Flags) error {
		c.Assert(s.Name(), Equals, "foo")
		c.Assert(s.SnapID, Equals, "snap-id")
		checkCbCalled = true
		return nil
	}
	r2 := snapstate.MockCheckSnapCallbacks([]snapstate.CheckSnapCallback{checkCb})
	defer r2()

	err := snapstate.CheckSnap(s.st, "snap-path", si, nil, snapstate.Flags{})
	c.Check(err, IsNil)

	c.Check(checkCbCalled, Equals, true)
}

func (s *checkSnapSuite) TestCheckSnapCheckCallbackFail(c *C) {
	const yaml = `name: foo
version: 1.0`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	fail := errors.New("bad snap")
	checkCb := func(st *state.State, s, cur *snap.Info, flags snapstate.Flags) error {
		return fail
	}
	r2 := snapstate.MockCheckSnapCallbacks(nil)
	defer r2()
	snapstate.AddCheckSnapCallback(checkCb)

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestCheckSnapGadgetCannotBeInstalledOnClassic(c *C) {
	reset := release.MockOnClassic(true)
	defer reset()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const yaml = `name: gadget
type: gadget
version: 1
`

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
	st.Lock()
	c.Check(err, ErrorMatches, "cannot install a gadget snap on classic")
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})

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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(true)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})

	c.Assert(err, ErrorMatches, ".* requires consent to use classic confinement")
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
`, "", si)
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
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
	st.Lock()
	c.Check(err, ErrorMatches, "cannot replace kernel snap with a different one")
}
