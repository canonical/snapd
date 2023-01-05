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

package dm_verity_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity/dm_verity"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type VerityTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&VerityTestSuite{})

func (s *VerityTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *VerityTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *VerityTestSuite) TestNewVeritySuperBlock(c *C) {
	sb := dm_verity.NewVeritySuperBlock()
	c.Check(sb.Version, Equals, uint64(0x1))
}

func (s *VerityTestSuite) TestParseVeritySetupOutput(c *C) {
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
	testsb := dm_verity.NewVeritySuperBlock()
	testsb.UUID = ""
	testsb.HashType = 1
	testsb.DataBlocks = 7
	testsb.DataBlockSize = 4096
	testsb.HashBlockSize = 4096
	testsb.Algorithm = "sha256"
	testsb.Salt = "595c3d19c4d8d56727332eba16ef6900faeb4fde0c6625fefcd178b8dfdff48a"

	testroothash := "cf9a379613c0dc10301fe3eba4665c38b849b7aad311471faa4d2392ee4ede49"

	parseVeritySetupOutput := dm_verity.ParseVeritySetupOutput
	roothash, sb := parseVeritySetupOutput([]byte(testinput))
	c.Check(sb, DeepEquals, &testsb)
	c.Check(roothash, Equals, testroothash)
}

func (s *VerityTestSuite) TestFormatNoSBSuccess(c *C) {
	dataDevice := "test.snap"
	hashDevice := "test.snap.verity"
	veritysetupformat := testutil.MockCommand(c, "veritysetup", "exit 0")
	defer veritysetupformat.Restore()

	_, _, err := dm_verity.FormatNoSB(dataDevice, hashDevice)
	c.Assert(err, IsNil)
	c.Check(veritysetupformat.Calls(), HasLen, 1)
}

func (s *VerityTestSuite) TestFormatNoSBFail(c *C) {
	dataDevice := "test.snap"
	hashDevice := "test.snap.verity"
	veritysetupformat := testutil.MockCommand(c, "veritysetup", "exit 1")
	defer veritysetupformat.Restore()

	_, _, err := dm_verity.FormatNoSB(dataDevice, hashDevice)
	c.Check(veritysetupformat.Calls(), HasLen, 1)
	c.Check(err, ErrorMatches, "exit status 1")
}
