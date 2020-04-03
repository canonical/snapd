// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type journalSuite struct {
	configcoreSuite
}

var _ = Suite(&journalSuite{})

func (s *journalSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	s.systemctlArgs = nil
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
}

func (s *journalSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *journalSuite) TestConfigurePersistentJournalInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{"journal.persistent": "foo"},
	})
	c.Assert(err, ErrorMatches, `journal.persistent can only be set to 'true' or 'false'`)
}

func (s *journalSuite) TestConfigurePersistentJournalOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{"journal.persistent": "true"},
	})
	c.Assert(err, IsNil)

	path := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/journald.conf.d/00-snap-core.conf")
	c.Check(path, testutil.FileEquals, "Storage=persistent\n")
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"stop", "systemd-journal"},
		{"show", "--property=ActiveState", "systemd-journal"},
		{"start", "systemd-journal"},
	})
}

func (s *journalSuite) TestDisablePersistentJournalOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{"journal.persistent": "false"},
	})
	c.Assert(err, IsNil)

	path := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/journald.conf.d/00-snap-core.conf")
	c.Check(path, testutil.FileEquals, "Storage=auto\n")
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"stop", "systemd-journal"},
		{"show", "--property=ActiveState", "systemd-journal"},
		{"start", "systemd-journal"},
	})
}

func (s *journalSuite) TestFilesystemOnlyApply(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"journal.persistent": "true",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(tmpDir, conf), IsNil)

	path := filepath.Join(tmpDir, "/etc/systemd/journald.conf.d/00-snap-core.conf")
	c.Check(path, testutil.FileEquals, "Storage=persistent\n")
}
