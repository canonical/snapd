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
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	"github.com/snapcore/snapd/overlord/snapstate"
)

type checkSnapSuite struct {
	testutil.BaseTest
	st *state.State
}

var _ = Suite(&checkSnapSuite{})

func (s *checkSnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.st = state.New(nil)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
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
			return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	restore = release.MockOnClassic(true)
	defer restore()

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{})

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

	err = snapstate.CheckSnap(s.st, "snap-path", nil, nil, snapstate.Flags{Classic: true})

	c.Assert(err, ErrorMatches, ".* requires classic confinement which is only available on classic systems")
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
		return info, emptyContainer(c), nil
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
		return info, emptyContainer(c), nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	st.Unlock()
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
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
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
	st.Lock()
	c.Check(err, ErrorMatches, "cannot find required base \"some-base\"")
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
`, "", si)
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
	err = snapstate.CheckSnap(st, "snap-path", nil, nil, snapstate.Flags{})
	st.Lock()
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestValidateContainerReallyEmptyFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	// the snap dir is a 0700 directory with nothing in it

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, snapdir.New(d))
	c.Check(err, Equals, snapstate.ErrMissingPaths)
}

func (s *checkSnapSuite) TestValidateContainerEmptyButBadPermFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()

	stat, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0700)) // just to be sure

	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0444), IsNil)

	// snapdir has /meta/snap.yaml, but / is 0700

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, snapdir.New(d))
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerMissingSnapYamlFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK), but no /meta/snap.yaml

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, snapdir.New(d))
	c.Check(err, Equals, snapstate.ErrMissingPaths)
}

func (s *checkSnapSuite) TestValidateContainerSnapYamlBadPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "meta", "snap.yaml"), nil, 0), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, but isn't readable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, snapdir.New(d))
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerSnapYamlNonRegularFails(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(syscall.Mkfifo(filepath.Join(d, "meta", "snap.yaml"), 0444), IsNil)

	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable, but isn't a file

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, snapdir.New(d))
	c.Check(err, Equals, snapstate.ErrBadModes)
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

func (s *checkSnapSuite) TestValidateContainerMinimalOKPermWorks(c *C) {
	const yaml = `name: empty-snap
version: 1
`
	d := emptyContainer(c)
	// snapdir's / and /meta are 0755 (i.e. OK),
	// /meta/snap.yaml exists, is readable regular file
	// (this could be considered a test of emptyContainer)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestValidateContainerMissingAppsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	// snapdir is empty: no apps

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrMissingPaths)
}

func (s *checkSnapSuite) TestValidateContainerBadAppPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "foo"), nil, 0444), IsNil)

	// snapdir contains the app, but the app is not executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerBadAppDirPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: apps/foo
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "apps"), 0700), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "apps", "foo"), nil, 0555), IsNil)

	// snapdir contains executable app, but path to executable isn't rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerBadSvcPermsFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 bar:
  command: svcs/bar
  daemon: simple
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "svcs"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "svcs", "bar"), nil, 0), IsNil)

	// snapdir contains service, but it isn't executable

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerCompleterFails(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: cmds/foo
  completer: comp/foo.sh
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "cmds"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "comp"), 0755), IsNil)

	// snapdir contains executable app, in a rx path, but refers
	// to a completer that doesn't exist

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrMissingPaths)
}

func (s *checkSnapSuite) TestValidateContainerBadAppPathOK(c *C) {
	// we actually support this, but don't validate it here
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: ../../../bin/echo
`
	d := emptyContainer(c)

	// snapdir does not contain the app, but the command is
	// "outside" so it might be OK

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestValidateContainerSymlinksFails(c *C) {
	c.Skip("checking symlink targets not implemented yet")
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	fn := filepath.Join(d.Path(), "foo")
	c.Assert(ioutil.WriteFile(fn+".real", nil, 0444), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's not world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, Equals, snapstate.ErrBadModes)
}

func (s *checkSnapSuite) TestValidateContainerSymlinksOK(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: foo
`
	d := emptyContainer(c)
	fn := filepath.Join(d.Path(), "foo")
	c.Assert(ioutil.WriteFile(fn+".real", nil, 0555), IsNil)
	c.Assert(os.Symlink(fn+".real", fn), IsNil)

	// snapdir contains a command that's a symlink to a file that's world-rx

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, IsNil)
}

func (s *checkSnapSuite) TestValidateContainerAppsOK(c *C) {
	const yaml = `name: empty-snap
version: 1
apps:
 foo:
  command: cmds/foo
  completer: comp/foo.sh
 bar:
  command: svcs/bar
  daemon: simple
 baz:
  command: cmds/foo --with=baz
 quux:
  command: cmds/foo
  daemon: simple
 meep:
  command: comp/foo.sh
  daemon: simple
`
	d := emptyContainer(c)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "cmds"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "cmds", "foo"), nil, 0555), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "comp"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "comp", "foo.sh"), nil, 0555), IsNil)

	c.Assert(os.Mkdir(filepath.Join(d.Path(), "svcs"), 0700), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d.Path(), "svcs", "bar"), nil, 0500), IsNil)

	c.Assert(os.Mkdir(filepath.Join(d.Path(), "garbage"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d.Path(), "garbage", "zero"), 0), IsNil)
	defer os.Chmod(filepath.Join(d.Path(), "garbage", "zero"), 0755)

	// snapdir contains:
	//  * a command that's world-rx, and its directory is
	//    world-rx, and its completer is world-r in a world-rx
	//    directory
	// * a service that's root-executable, and its directory is
	//   not readable nor searchable - and that's OK! (NOTE as
	//   this test should pass as non-rooot, the directory is 0700
	//   instead of 0000)
	// * a command with arguments
	// * a service that is also a command
	// * a service that is also a completer (WAT)
	// * an extra directory only root can look at (this would fail
	//   if not running the suite as root, and SkipDir didn't
	//   work)

	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	err = snapstate.ValidateContainer(info, d)
	c.Check(err, IsNil)
}
