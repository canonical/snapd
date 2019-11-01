// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type firstbootPreseed16Suite struct {
	firstBootBaseTest

	// TestingSeed16 helps populating seeds (it provides
	// MakeAssertedSnap, WriteAssertions etc.) for tests.
	*seedtest.TestingSeed16
}

var _ = Suite(&firstbootPreseed16Suite{})

func checkPreseedTasks(c *C, tsAll []*state.TaskSet) {
	// the tasks of the last taskset must be gadget-connect, mark-preseeded, mark-seeded
	lastTasks := tsAll[len(tsAll)-1].Tasks()
	c.Check(lastTasks, HasLen, 3)
	gadgetConnectTask := lastTasks[0]
	preseedTask := lastTasks[1]
	markSeededTask := lastTasks[2]
	c.Check(gadgetConnectTask.Kind(), Equals, "gadget-connect")
	c.Check(preseedTask.Kind(), Equals, "mark-preseeded")
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")

	// mark-seeded waits for mark-preseeded and gadget-connect
	c.Check(markSeededTask.WaitTasks(), DeepEquals, []*state.Task{gadgetConnectTask, preseedTask})
}

func (s *firstbootPreseed16Suite) makeSeedChange(c *C, st *state.State) *state.Change {
	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	s.WriteAssertions("developer.account", s.devAcct)

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.snap-declaration", fooDecl)
	s.WriteAssertions("foo.snap-revision", fooRev)

	// put a firstboot local snap into the SnapBlobDir
	snapYaml = `name: local
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile2 := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	err := os.Rename(mockSnapFile, targetSnapFile2)
	c.Assert(err, IsNil)

	// add a model assertion and its chain
	assertsChain := s.MakeModelAssertionChain("my-brand", "my-model", modelHeaders("my-model", "foo"), nil)
	for i, as := range assertsChain {
		s.WriteAssertions(strconv.Itoa(i), as)
	}

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: foo
   file: %s
   devmode: true
   contact: mailto:some.guy@example.com
 - name: local
   unasserted: true
   file: %s
`, coreFname, kernelFname, gadgetFname, fooFname, filepath.Base(targetSnapFile2)))
	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st, true, s.perfTimings)
	c.Assert(err, IsNil)

	// XXX: order is more convoluted in preseed mode; implement a check for it.
	//checkOrder(c, tsAll, "core", "pc-kernel", "pc", "foo", "local")

	checkPreseedTasks(c, tsAll)

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	return chg
}

func (s *firstbootPreseed16Suite) makeCoreSnaps(c *C, extraGadgetYaml string) (coreFname, kernelFname, gadgetFname string) {
	files := [][]string{}
	if strings.Contains(extraGadgetYaml, "defaults:") {
		files = [][]string{{"meta/hooks/configure", ""}}
	}

	// put core snap into the SnapBlobDir
	snapYaml := `name: core
version: 1.0
type: os`
	coreFname, coreDecl, coreRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("core.asserts", coreRev, coreDecl)

	// put kernel snap into the SnapBlobDir
	snapYaml = `name: pc-kernel
version: 1.0
type: kernel`
	kernelFname, kernelDecl, kernelRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("kernel.asserts", kernelRev, kernelDecl)

	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	gadgetYaml += extraGadgetYaml

	// put gadget snap into the SnapBlobDir
	files = append(files, []string{"meta/gadget.yaml", gadgetYaml})

	snapYaml = `name: pc
version: 1.0
type: gadget`
	gadgetFname, gadgetDecl, gadgetRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("gadget.asserts", gadgetRev, gadgetDecl)

	return coreFname, kernelFname, gadgetFname
}

func (s *firstbootPreseed16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}

	s.setupBaseTest(c, &s.TestingSeed16.SeedSnaps, false)

	s.SeedDir = dirs.SnapSeedDir

	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)
}

func (s *firstbootPreseed16Suite) TestPreseedHappy(c *C) {
	restore := release.MockPreseedMode(true)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	systemd.MockOsSymlink(func(string, string) error { return nil })

	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	o := s.createOverlord(c)
	st := o.State()
	chg := s.makeSeedChange(c, st)
	err := o.Settle(settleTimeout)

	st.Lock()
	defer st.Unlock()

	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	checkPressedTaskStates(c, st)
}
