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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity/dmverity"
	"github.com/snapcore/snapd/snap/snaptest"
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

func (vs *VerityTestSuite) makeValidVeritySetupOutput() string {
	return `
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
}

func (s *VerityTestSuite) TestGetRootHashFromOutput(c *C) {
	testinput := s.makeValidVeritySetupOutput()
	testroothash := "cf9a379613c0dc10301fe3eba4665c38b849b7aad311471faa4d2392ee4ede49"

	roothash, err := dmverity.GetRootHashFromOutput([]byte(testinput))
	c.Assert(err, IsNil)
	c.Check(roothash, Equals, testroothash)
}

func (s *VerityTestSuite) TestGetRootHashFromOutputInvalid(c *C) {
	validVeritySetupOutput := s.makeValidVeritySetupOutput()

	rootHashLine := "Root hash:      	cf9a379613c0dc10301fe3eba4665c38b849b7aad311471faa4d2392ee4ede49"
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{rootHashLine, "", "internal error: unexpected root hash length"},
		{rootHashLine, "Root hash      	", "internal error: unexpected veritysetup output format"},
		{"Hash algorithm:  	sha256", "Hash algorithm:  	sha25", "internal error: unexpected hash algorithm"},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(validVeritySetupOutput, test.original, test.invalid, 1)
		_, err := dmverity.GetRootHashFromOutput([]byte(invalid))
		c.Check(err, ErrorMatches, test.expectedErr)
	}
}

func (s *VerityTestSuite) TestFormatSuccess(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	// mock the verity-setup command, what it does is make a copy of the snap
	// and then returns pre-calculated output
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		cp %[1]s %[1]s.verity
		echo VERITY header information for %[1]s.verity
		echo "UUID:            	93740d5e-9039-4a07-9219-bd355882b64b"
		echo "Hash type:       	1"
		echo "Data blocks:     	2048"
		echo "Data block size: 	4096"
		echo "Hash blocks:     	17"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	46aee3affbd0455623e907bb7fc622999bac4c86fa263808ac15240b16286458"
		echo "Root hash:      	9257053cde92608d275cd912c031c40dd9d8820e4645f0774ec2d4403f19f840"
		echo "Hash device size: 73728 [bytes]"
		;;
esac
`, snapPath))
	defer vscmd.Restore()

	rootHash, err := dmverity.Format(snapPath, snapPath+".verity", nil)
	c.Assert(err, IsNil)
	c.Assert(vscmd.Calls(), HasLen, 2)
	c.Check(vscmd.Calls()[0], DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], DeepEquals, []string{"veritysetup", "format", snapPath, snapPath + ".verity"})

	c.Check(rootHash, Equals, "9257053cde92608d275cd912c031c40dd9d8820e4645f0774ec2d4403f19f840")
}

func (s *VerityTestSuite) TestFormatSuccessWithWorkaround(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	// use a version that forces the deployment of the workaround to run. Any version
	// before 2.0.4 should automatically create a file we can verify
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 1.6.4"
		exit 0
		;;
	format)
		if ! [ -e %[1]s.verity ]; then
			exit 1
		fi
		echo VERITY header information for %[1]s.verity
		echo "UUID:            	97d80536-aad9-4f25-a528-5319c038c0c4"
		echo "Hash type:       	1"
		echo "Data blocks:     	1"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	c0234a906cfde0d5ffcba25038c240a98199cbc1d8fbd388a41e8faa02239c08"
		echo "Root hash:      	e48cfc4df6df0f323bcf67f17b659a5074bec3afffe28f0b3b4db981d78d2e3e"
		;;
esac
`, snapPath))
	defer vscmd.Restore()

	_, err := dmverity.Format(snapPath, snapPath+".verity", nil)
	c.Assert(err, IsNil)
	c.Assert(vscmd.Calls(), HasLen, 2)
	c.Check(vscmd.Calls()[0], DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], DeepEquals, []string{"veritysetup", "format", snapPath, snapPath + ".verity"})
}

func (s *VerityTestSuite) TestFormatVerityFails(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)
	vscmd := testutil.MockCommand(c, "veritysetup", `
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		echo "Cannot create hash image $3 for writing."
		exit 1
		;;
esac
`)
	defer vscmd.Restore()

	rootHash, err := dmverity.Format(snapPath, "", nil)
	c.Assert(rootHash, Equals, "")
	c.Check(err, ErrorMatches, "Cannot create hash image  for writing.")
}

func (s *VerityTestSuite) TestVerityVersionDetect(c *C) {
	tests := []struct {
		ver    string
		deploy bool
		err    string
	}{
		{"", false, `cannot detect veritysetup version from: veritysetup\n`},
		{"1", false, `cannot detect veritysetup version from: veritysetup 1\n`},
		{"1.6", false, `cannot detect veritysetup version from: veritysetup 1.6\n`},
		{"1.6.4", true, ``},
		{"2.0.0", true, ``},
		{"2.0.4", false, ``},
		{"2.1.0", false, ``},
	}

	for _, t := range tests {
		vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`echo veritysetup %s`, t.ver))
		defer vscmd.Restore()

		deploy, err := dmverity.ShouldApplyWorkaround()
		if err != nil {
			c.Check(err, ErrorMatches, t.err, Commentf("test failed for version: %s", t.ver))
		}
		c.Check(deploy, Equals, t.deploy, Commentf("test failed for version: %s", t.ver))
	}
}

func (s *VerityTestSuite) TestReadSuperblockSuccess(c *C) {
	// testdisk.verity is generated by:
	// - dd if=/dev/zero of=testdisk bs=8M count=1
	// - veritysetup format testdisk testdisk.verity
	sb, err := dmverity.ReadSuperblock("testdata/testdisk.verity")
	c.Check(err, IsNil)

	sbJson, _ := json.Marshal(sb)
	expectedSb := "{\"Signature\":[118,101,114,105,116,121,0,0]," +
		"\"Version\":1," +
		"\"HashType\":1," +
		"\"Uuid\":[147,116,13,94,144,57,74,7,146,25,189,53,88,130,182,75]," +
		"\"Algorithm\":[115,104,97,50,53,54,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]," +
		"\"DataBlockSize\":4096," +
		"\"HashBlockSize\":4096," +
		"\"DataBlocks\":2048," +
		"\"SaltSize\":32," +
		"\"Pad1\":[0,0,0,0,0,0]," +
		"\"Salt\":[70,174,227,175,251,208,69,86,35,233,7,187,127,198,34,153,155,172,76,134,250,38,56,8,172,21,36,11,22,40,100,88,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]," +
		"\"Pad2\":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}"
	c.Check(string(sbJson), Equals, expectedSb)
}

func (s *VerityTestSuite) TestReadEmptySuperBlockError(c *C) {
	// Attempt to read an empty disk

	// create empty file
	testDiskPath := filepath.Join(c.MkDir(), "testdisk")
	f, err := os.Create(testDiskPath)
	c.Assert(err, IsNil)
	defer f.Close()

	err = os.Truncate(testDiskPath, 8*1024*1024)
	c.Assert(err, IsNil)

	// attempt to read superblock from it
	_, err = dmverity.ReadSuperblock(testDiskPath)
	c.Check(err, ErrorMatches, "invalid dm-verity superblock version")
}

func (s *VerityTestSuite) TestVeritySuperblockEncodedSalt(c *C) {
	sbJson := `{"version":1,"hashType":1,"uuid":[147,116,13,94,144,57,74,7,146,25,189,53,88,130,182,75],"algorithm":[115,104,97,50,53,54,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"dataBlockSize":4096,"hashBlockSize":4096,"dataBlocks":2048,"saltSize":32,"salt":[70,174,227,175,251,208,69,86,35,233,7,187,127,198,34,153,155,172,76,134,250,38,56,8,172,21,36,11,22,40,100,88,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}`

	var sb dmverity.VeritySuperblock
	err := json.Unmarshal([]byte(sbJson), &sb)
	c.Assert(err, IsNil)

	c.Check(sb.EncodedSalt(), Equals, "46aee3affbd0455623e907bb7fc62299")
}
