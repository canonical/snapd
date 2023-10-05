// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/wrappers"
)

func TestWrappers(t *testing.T) { TestingT(t) }

type binariesTestSuite struct {
	tempdir string
	base    string
}

// silly wrappers to get better failure messages
type noBaseBinariesSuite struct{ binariesTestSuite }
type withBaseBinariesSuite struct{ binariesTestSuite }
type withSnapdBinariesSuite struct{ binariesTestSuite }

var _ = Suite(&noBaseBinariesSuite{})
var _ = Suite(&withBaseBinariesSuite{binariesTestSuite{base: "core99"}})
var _ = Suite(&withSnapdBinariesSuite{binariesTestSuite{base: "core-with-snapd"}})

func (s *binariesTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.BashCompletionScript), 0755), IsNil)
	f, err := os.OpenFile(dirs.BashCompletionScript, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	f.Write([]byte("#\nBASH_COMPLETION_VERSINFO=(2 6)\n"))
	f.Close()
}

func (s *binariesTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

const packageHelloNoSrv = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
 hello:
   command: bin/hello
 world:
   command: bin/world
   completer: world-completer.sh
`

const packageHello = packageHelloNoSrv + `
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`

const packageHelloNoSrvV2 = `name: hello-snap
version: 1.11
summary: hello
description: Hello...
apps:
 hello:
   command: bin/hello
 universe:
   command: bin/universe
   completer: universe-completer.sh
`

const packageHelloV2 = packageHelloNoSrvV2 + `
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`

func (s *binariesTestSuite) TestAddSnapBinariesAndRemove(c *C) {
	// no completers support -> no problem \o/
	c.Assert(osutil.FileExists(dirs.CompletersDir), Equals, false)

	s.testAddSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemove(c *C) {
	// no completers support -> no problem \o/
	c.Assert(osutil.FileExists(dirs.CompletersDir), Equals, false)

	s.testEnsureSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) prepareReadOnlyLegacyDir(c *C) {
	c.Assert(os.MkdirAll(dirs.LegacyCompletersDir, 0400), IsNil)
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}

	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)

	f, err := os.OpenFile(dirs.BashCompletionScript, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	f.Write([]byte("#\n#   RELEASE: 2.1\n"))
	f.Close()
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveReadOnlyLegacyDir(c *C) {
	s.prepareReadOnlyLegacyDir(c)
	s.testAddSnapBinariesAndRemove(c, true, true)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveReadOnlyLegacyDir(c *C) {
	s.prepareReadOnlyLegacyDir(c)
	s.testEnsureSnapBinariesAndRemove(c, true, true)
}

func (s *binariesTestSuite) prepareUseLegacy(c *C) {
	c.Assert(os.MkdirAll(dirs.LegacyCompletersDir, 0755), IsNil)
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}

	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)

	f, err := os.OpenFile(dirs.BashCompletionScript, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	f.Write([]byte("#\n#   RELEASE: 2.1\n"))
	f.Close()
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveUseLegacy(c *C) {
	s.prepareUseLegacy(c)
	s.testAddSnapBinariesAndRemove(c, true, false)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveUseLegacy(c *C) {
	s.prepareUseLegacy(c)
	s.testEnsureSnapBinariesAndRemove(c, true, false)
}

func (s *binariesTestSuite) prepareOldButNotThatOld(c *C) {
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}

	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)

	f, err := os.OpenFile(dirs.BashCompletionScript, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	f.Write([]byte("#\n#   RELEASE: 2.2\n"))
	f.Close()
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveOldButNotThatOld(c *C) {
	s.prepareOldButNotThatOld(c)
	s.testAddSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveOldButNotThatOld(c *C) {
	s.prepareOldButNotThatOld(c)
	s.testEnsureSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) prepareUnknownVersion(c *C) {
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}

	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)

	f, err := os.OpenFile(dirs.BashCompletionScript, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	f.Close()
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveUnknownVersion(c *C) {
	s.prepareUnknownVersion(c)
	s.testAddSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveUnknownVersion(c *C) {
	s.prepareUnknownVersion(c)
	s.testEnsureSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) prepareNotInstalled(c *C) {
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}

	c.Assert(os.Remove(dirs.BashCompletionScript), IsNil)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveNotInstalled(c *C) {
	s.prepareNotInstalled(c)
	s.testAddSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveNotInstalled(c *C) {
	s.prepareNotInstalled(c)
	s.testEnsureSnapBinariesAndRemove(c, false, true)
}

func (s *binariesTestSuite) prepareWithCompleters(c *C) {
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithCompleters(c *C) {
	s.prepareWithCompleters(c)
	// full completers support -> we get completers \o/
	s.testAddSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveWithCompleters(c *C) {
	s.prepareWithCompleters(c)
	// full completers support -> we get completers \o/
	s.testEnsureSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithLegacyCompleters(c *C) {
	c.Assert(os.MkdirAll(dirs.LegacyCompletersDir, 0755), IsNil)
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)
	c.Assert(os.Symlink(dirs.CompleteShPath(s.base), filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")), IsNil)

	s.testAddSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) TestRemoveWithLegacyCompleters(c *C) {
	info := snaptest.MockSnap(c, packageHello+"base: "+s.base+"\n", &snap.SideInfo{Revision: snap.R(11)})
	err := wrappers.EnsureSnapBinaries(info)
	c.Assert(err, IsNil)

	// Simulate legacy installation
	newCompleter := filepath.Join(dirs.CompletersDir, "hello-snap.world")
	if osutil.FileExists(newCompleter) {
		c.Assert(os.Remove(newCompleter), IsNil)
	}
	c.Assert(os.MkdirAll(dirs.LegacyCompletersDir, 0755), IsNil)
	legacyCompleter := filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")
	c.Assert(os.Symlink(dirs.CompleteShPath(s.base), legacyCompleter), IsNil)

	err = wrappers.RemoveSnapBinaries(info)
	c.Assert(err, IsNil)

	c.Assert(osutil.IsSymlink(legacyCompleter), Equals, false)
}

func (s *binariesTestSuite) prepareWithExistingCompleters(c *C) {
	c.Assert(os.MkdirAll(dirs.CompletersDir, 0755), IsNil)
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)
	// existing completers -> they're left alone \o/
	c.Assert(os.WriteFile(filepath.Join(dirs.CompletersDir, "hello-snap.world"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.CompletersDir, "hello-snap.universe"), nil, 0644), IsNil)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithExistingCompleters(c *C) {
	s.prepareWithExistingCompleters(c)
	s.testAddSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveWithExistingCompleters(c *C) {
	s.prepareWithExistingCompleters(c)
	s.testEnsureSnapBinariesAndRemove(c, false, false)
}

func (s *binariesTestSuite) prepareWithExistingLegacyCompleters(c *C) {
	c.Assert(os.MkdirAll(dirs.LegacyCompletersDir, 0755), IsNil)
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)
	// existing completers -> they're left alone \o/
	c.Assert(os.WriteFile(filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.LegacyCompletersDir, "hello-snap.universe"), nil, 0644), IsNil)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithExistingLegacyCompleters(c *C) {
	s.prepareWithExistingLegacyCompleters(c)
	s.testAddSnapBinariesAndRemove(c, true, false)
}

func (s *binariesTestSuite) TestEnsureSnapBinariesAndRemoveWithExistingLegacyCompleters(c *C) {
	s.prepareWithExistingLegacyCompleters(c)
	s.testEnsureSnapBinariesAndRemove(c, true, false)
}

func (s *binariesTestSuite) testAddSnapBinariesAndRemove(c *C, useLegacy bool, disabledCompletionOnHost bool) {
	info := snaptest.MockSnap(c, packageHello+"base: "+s.base+"\n", &snap.SideInfo{Revision: snap.R(11)})
	completer := filepath.Join(dirs.CompletersDir, "hello-snap.world")
	if useLegacy {
		completer = filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")
	}
	completerExisted := osutil.FileExists(completer) && !osutil.IsSymlink(completer)

	err := wrappers.EnsureSnapBinaries(info)
	c.Assert(err, IsNil)

	bins := []string{"hello-snap.hello", "hello-snap.world"}

	for _, bin := range bins {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		target, err := os.Readlink(link)
		c.Assert(err, IsNil, Commentf(bin))
		c.Check(target, Equals, "/usr/bin/snap", Commentf(bin))
	}

	compDir := dirs.CompletersDir
	if useLegacy {
		compDir = dirs.LegacyCompletersDir
	}
	if disabledCompletionOnHost || !osutil.FileExists(dirs.CompleteShPath(s.base)) {
		c.Assert(osutil.IsSymlink(filepath.Join(dirs.CompletersDir, "hello-snap.world")), Equals, false)
		c.Assert(osutil.IsSymlink(filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")), Equals, false)
	} else {
		c.Assert(osutil.FileExists(dirs.CompleteShPath(s.base)), Equals, true)
		c.Assert(osutil.IsDirectory(compDir), Equals, true)

		if completerExisted {
			// there was a completer there before, so it should _not_ be a symlink to our complete.sh
			c.Check(osutil.FileExists(completer), Equals, true)
			c.Assert(osutil.IsSymlink(completer), Equals, false)
		} else {
			target, err := os.Readlink(completer)
			c.Assert(err, IsNil)
			c.Check(target, Equals, dirs.CompleteShPath(s.base))
		}
	}

	if !disabledCompletionOnHost && !useLegacy {
		legacyCompleter := filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")
		c.Assert(osutil.IsSymlink(legacyCompleter), Equals, false)
	}

	err = wrappers.RemoveSnapBinaries(info)
	c.Assert(err, IsNil)

	for _, bin := range bins {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		c.Check(osutil.IsSymlink(link), Equals, false, Commentf(bin))
	}

	// we left the existing completer alone, but removed it otherwise
	c.Check(osutil.FileExists(completer), Equals, completerExisted)
}

func (s *binariesTestSuite) testEnsureSnapBinariesAndRemove(c *C, useLegacy bool, disabledCompletionOnHost bool) {
	oldInfo := snaptest.MockSnap(c, packageHello+"base: "+s.base+"\n", &snap.SideInfo{Revision: snap.R(11)})
	oldCompleter := filepath.Join(dirs.CompletersDir, "hello-snap.world")
	if useLegacy {
		oldCompleter = filepath.Join(dirs.LegacyCompletersDir, "hello-snap.world")
	}
	oldCompleterExisted := osutil.FileExists(oldCompleter) && !osutil.IsSymlink(oldCompleter)
	err := wrappers.EnsureSnapBinaries(oldInfo)
	c.Assert(err, IsNil)

	newInfo := snaptest.MockSnap(c, packageHelloV2+"base: "+s.base+"\n", &snap.SideInfo{Revision: snap.R(12)})
	newCompleter := filepath.Join(dirs.CompletersDir, "hello-snap.universe")
	if useLegacy {
		newCompleter = filepath.Join(dirs.LegacyCompletersDir, "hello-snap.universe")
	}
	newCompleterExisted := osutil.FileExists(newCompleter) && !osutil.IsSymlink(newCompleter)
	err = wrappers.EnsureSnapBinaries(newInfo)
	c.Assert(err, IsNil)

	binsAdded := []string{"hello-snap.hello", "hello-snap.universe"}
	binsRemoved := []string{"hello-snap.world"}

	for _, bin := range binsAdded {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		target, err := os.Readlink(link)
		c.Assert(err, IsNil, Commentf(bin))
		c.Check(target, Equals, "/usr/bin/snap", Commentf(bin))
	}

	for _, bin := range binsRemoved {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		c.Check(osutil.IsSymlink(link), Equals, false, Commentf(bin))
	}

	compDir := dirs.CompletersDir
	if useLegacy {
		compDir = dirs.LegacyCompletersDir
	}

	if oldCompleterExisted {
		// there was a completer there before, so it should _not_ be removed or be a symlink to our complete.sh
		c.Check(osutil.FileExists(oldCompleter), Equals, true)
		c.Check(osutil.IsSymlink(oldCompleter), Equals, false)
	} else {
		// we created this completer, old revision completer should be removed
		c.Check(osutil.FileExists(oldCompleter), Equals, false)
	}

	if disabledCompletionOnHost || !osutil.FileExists(dirs.CompleteShPath(s.base)) {
		c.Assert(osutil.IsSymlink(filepath.Join(dirs.CompletersDir, "hello-snap.universe")), Equals, false)
		c.Assert(osutil.IsSymlink(filepath.Join(dirs.LegacyCompletersDir, "hello-snap.universe")), Equals, false)
	} else {
		c.Assert(osutil.FileExists(dirs.CompleteShPath(s.base)), Equals, true)
		c.Assert(osutil.IsDirectory(compDir), Equals, true)

		if newCompleterExisted {
			// there was a completer there before, so it should _not_ be a symlink to our complete.sh
			c.Check(osutil.FileExists(newCompleter), Equals, true)
			c.Assert(osutil.IsSymlink(newCompleter), Equals, false)
		} else {
			target, err := os.Readlink(newCompleter)
			c.Assert(err, IsNil)
			c.Check(target, Equals, dirs.CompleteShPath(s.base))
		}
	}

	if !disabledCompletionOnHost && !useLegacy {
		legacyCompleter := filepath.Join(dirs.LegacyCompletersDir, "hello-snap.universe")
		c.Assert(osutil.IsSymlink(legacyCompleter), Equals, false)
	}

	err = wrappers.RemoveSnapBinaries(newInfo)
	c.Assert(err, IsNil)

	for _, bin := range binsAdded {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		c.Check(osutil.IsSymlink(link), Equals, false, Commentf(bin))
	}

	// we left the existing completer alone, but removed it otherwise
	c.Check(osutil.FileExists(newCompleter), Equals, newCompleterExisted)
}

func (s *binariesTestSuite) TestAddSnapBinariesCleansUpOnFailure(c *C) {
	link := filepath.Join(dirs.SnapBinariesDir, "hello-snap.hello")
	c.Assert(osutil.FileExists(link), Equals, false)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBinariesDir, "hello-snap.bye", "potato"), 0755), IsNil)

	info := snaptest.MockSnap(c, packageHello+`
 bye:
  command: bin/bye
`, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.EnsureSnapBinaries(info)
	c.Assert(err, NotNil)

	c.Check(osutil.FileExists(link), Equals, false)
}

func (s *iconsTestSuite) TestEnsureSnapBinariesNilSnapInfo(c *C) {
	c.Assert(wrappers.EnsureSnapBinaries(nil), ErrorMatches, "internal error: snap info cannot be nil")
}
