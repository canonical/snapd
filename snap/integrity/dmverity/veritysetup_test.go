// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package dmverity_test

import (
	"fmt"
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity/dmverity"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type VerityTestSuite struct {
	testutil.BaseTest

	veritysetup *testutil.MockCmd
}

var _ = Suite(&VerityTestSuite{})

func (s *VerityTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	veritysetupWrapper := `exec %[1]s "$@" </dev/stdin`

	veritysetup, err := exec.LookPath("veritysetup")
	c.Assert(err, IsNil)

	s.veritysetup = testutil.MockCommand(c, "veritysetup", fmt.Sprintf(veritysetupWrapper, veritysetup))
	s.AddCleanup(s.veritysetup.Restore)
}

func (s *VerityTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *VerityTestSuite) TestGetRootHashFromOutput(c *C) {
	const testinput = `
VERITY header information for my-snap-name_0.1_all.snap.veritynosb
UUID:
Hash type:       	1
Data blocks:     	7
Data block size: 	4096
Hash blocks:     	1
Hash block size: 	4096
Hash algorithm:  	sha256
Salt:            	595c3d19c4d8d56727332eba16ef6900faeb4fde0c6625fefcd178b8dfdff48a
Root hash:      	cf9a379613c0dc10301fe3eba4665c38b849b7aad311471faa4d2392ee4ede49
Hash device size: 	4096 [bytes]
`
	testroothash := "cf9a379613c0dc10301fe3eba4665c38b849b7aad311471faa4d2392ee4ede49"

	roothash, err := dmverity.GetRootHashFromOutput([]byte(testinput))
	c.Assert(err, IsNil)
	c.Check(roothash, Equals, testroothash)
}

func (s *VerityTestSuite) TestGetRootHashFromOutputNoRootHash(c *C) {
	const testinput = `
VERITY header information for my-snap-name_0.1_all.snap.veritynosb
UUID:
Hash type:       	1
Data blocks:     	7
Data block size: 	4096
Hash blocks:     	1
Hash block size: 	4096
Hash algorithm:  	sha256
Salt:            	595c3d19c4d8d56727332eba16ef6900faeb4fde0c6625fefcd178b8dfdff48a
Hash device size: 	4096 [bytes]
`

	_, err := dmverity.GetRootHashFromOutput([]byte(testinput))
	c.Check(err, ErrorMatches, `empty root hash`)
}

func (s *VerityTestSuite) TestFormatSuccess(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	_, err := dmverity.Format(snapPath, snapPath+".verity")
	c.Assert(err, IsNil)
	c.Check(s.veritysetup.Calls(), HasLen, 1)

	// [1:] to ignore Exe() which is the tmp path for cryptsetup from the mocking
	c.Check(s.veritysetup.Calls()[1:], DeepEquals, [][]string{{s.veritysetup.Exe(), "format", snapPath, snapPath + ".verity"}}[1:])
}

func (s *VerityTestSuite) TestFormatFail(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	_, err := dmverity.Format(snapPath, "")
	c.Check(err, ErrorMatches, "Cannot create hash image  for writing.")
}
