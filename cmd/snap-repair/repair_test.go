// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type repairSuite struct {
	testutil.BaseTest
	root string

	origFindRepairAssertions func() ([]asserts.Assertion, error)
	origOnClassic            bool

	cmds [][]string

	repairs []asserts.Assertion
}

var _ = Suite(&repairSuite{})

var script = `#!/bin/sh
echo "hello world"
`

var mockRepair = fmt.Sprintf(`type: repair
authority-id: canonical
repair-id: REPAIR-42
series:
  - 16
body-length: %v
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

%s

AXNpZw==`, len(script), script)

func (s *repairSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.root = c.MkDir()
	s.origFindRepairAssertions = findRepairAssertions
	s.AddCleanup(release.MockOnClassic(false))

	findRepairAssertions = func() ([]asserts.Assertion, error) {
		return s.repairs, nil
	}
	execCommand = func(name string, arg ...string) *exec.Cmd {
		cmd := []string{name}
		cmd = append(cmd, arg...)
		s.cmds = append(s.cmds, cmd)
		return exec.Command(name, arg...)
	}
	dirs.SetRootDir(s.root)
}

func (s *repairSuite) TearDown(c *C) {
	s.BaseTest.TearDownTest(c)

	dirs.SetRootDir("/")
	execCommand = exec.Command
	findRepairAssertions = s.origFindRepairAssertions
}

func (s *repairSuite) TestRunNoRepairs(c *C) {
	err := runRepair()
	c.Check(err, IsNil)
	c.Check(s.cmds, HasLen, 0)
}

func (s *repairSuite) TestRunSingleRepair(c *C) {
	repair, err := asserts.Decode([]byte(mockRepair))
	c.Assert(err, IsNil)

	s.repairs = []asserts.Assertion{repair}
	err = runRepair()
	c.Check(err, IsNil)
	c.Check(s.cmds, HasLen, 1)
	c.Check(s.cmds, DeepEquals, [][]string{
		{filepath.Join(dirs.SnapRepairDir, "REPAIR-42", "script")},
	})
	output, err := ioutil.ReadFile(filepath.Join(dirs.SnapRepairDir, "REPAIR-42/REPAIR-42.output"))
	c.Assert(err, IsNil)
	c.Check(string(output), Equals, "hello world\n")

	// run again and ensure the already done repair is skipped
	err = runRepair()
	c.Check(err, IsNil)
	c.Check(s.cmds, HasLen, 1)
}
