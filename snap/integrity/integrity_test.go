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

package integrity_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/snap/integrity/dmverity"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type IntegrityTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&IntegrityTestSuite{})

func (s *IntegrityTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *IntegrityTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *IntegrityTestSuite) TestAlign(c *C) {
	align := integrity.Align
	blockSize := uint64(integrity.BlockSize)

	for _, tc := range []struct {
		input          uint64
		expectedOutput uint64
	}{
		{0, 0},
		{1, blockSize},
		{blockSize, blockSize},
		{blockSize + 1, 2 * blockSize},
	} {
		ret := align(tc.input)
		c.Check(ret, Equals, tc.expectedOutput, Commentf("%v", tc))
	}
}

func (s *IntegrityTestSuite) TestIntegrityHeaderMarshalJSON(c *C) {
	dmVerityBlock := &dmverity.Info{}
	integrityDataHeader := integrity.NewIntegrityDataHeader(dmVerityBlock, 4096)

	jsonHeader, err := json.Marshal(integrityDataHeader)
	c.Assert(err, IsNil)

	c.Check(json.Valid(jsonHeader), Equals, true)

	expected := []byte(`{"type":"integrity","size":"8192","dm-verity":{"root-hash":""}}`)
	c.Check(jsonHeader, DeepEquals, expected)
}

func (s *IntegrityTestSuite) TestIntegrityHeaderUnmarshalJSON(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`

	err := json.Unmarshal([]byte(integrityHeaderJSON), &integrityDataHeader)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(4096))
	c.Check(integrityDataHeader.DmVerity.RootHash, Equals, "00000000000000000000000000000000")
}

func (s *IntegrityTestSuite) TestIntegrityHeaderEncode(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	header, err := integrityDataHeader.Encode()
	c.Assert(err, IsNil)

	magicRead := header[0:len(magic)]
	c.Check(magicRead, DeepEquals, magic)

	nullByte := header[len(header)-1:]
	c.Check(nullByte, DeepEquals, []byte{0x0})

	c.Check(uint64(len(header)), Equals, integrity.Align(uint64(len(header))))
}

func (s *IntegrityTestSuite) TestIntegrityHeaderEncodeInvalidSize(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	integrityDataHeader.Type = strings.Repeat("a", integrity.BlockSize)

	_, err := integrityDataHeader.Encode()
	c.Assert(err, ErrorMatches, "internal error: invalid integrity data header: wrong size")
}

func (s *IntegrityTestSuite) TestIntegrityHeaderDecode(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`
	header := append(magic, integrityHeaderJSON...)
	header = append(header, 0)

	headerBlock := make([]byte, 4096)
	copy(headerBlock, header)

	err := integrityDataHeader.Decode(headerBlock)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(4096))
	c.Check(integrityDataHeader.DmVerity.RootHash, Equals, "00000000000000000000000000000000")
}

func (s *IntegrityTestSuite) TestIntegrityHeaderDecodeInvalidMagic(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := []byte("invalid")

	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`
	header := append(magic, integrityHeaderJSON...)
	header = append(header, 0)

	headerBlock := make([]byte, 4096)
	copy(headerBlock, header)

	err := integrityDataHeader.Decode(headerBlock)
	c.Check(err, ErrorMatches, "invalid integrity data header: invalid magic value")
}

func (s *IntegrityTestSuite) TestIntegrityHeaderDecodeInvalidJSON(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	integrityHeaderJSON := `
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`
	header := append(magic, integrityHeaderJSON...)
	header = append(header, 0)

	headerBlock := make([]byte, 4096)
	copy(headerBlock, header)

	err := integrityDataHeader.Decode(headerBlock)

	_, ok := err.(*json.SyntaxError)
	c.Check(ok, Equals, true)
}

func (s *IntegrityTestSuite) TestIntegrityHeaderDecodeInvalidTermination(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`
	header := append(magic, integrityHeaderJSON...)

	headerBlock := make([]byte, len(header))
	copy(headerBlock, header)

	err := integrityDataHeader.Decode(headerBlock)
	c.Check(err, ErrorMatches, "invalid integrity data header: no null byte found at end of input")
}

func (s *IntegrityTestSuite) TestGenerateAndAppendSuccess(c *C) {
	blockSize := uint64(integrity.BlockSize)

	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	// 8192 is the hash size that is created when running 'veritysetup format'
	// on a minimally sized snap. there is not an easy way to calculate this
	// value dynamically.
	const verityHashSize = 8192

	// mock the verity-setup command, what it does is make a copy of the snap
	// and then returns pre-calculated output
	vscmd := testutil.MockCommand(c, "veritysetup", fmt.Sprintf(`
case "$1" in
	--version)
		echo "veritysetup 2.2.6"
		exit 0
		;;
	format)
		truncate -s %[1]d %[2]s.verity
		echo "VERITY header information for %[2]s.verity"
		echo "UUID:            	f8b4f201-fe4e-41a2-9f1d-4908d3c76632"
		echo "Hash type:       	1"
		echo "Data blocks:     	4"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	f1a7f87b88692b388f47dbda4a3bdf790f5adc3104b325f8772aee593488bf15"
		echo "Root hash:      	e2926364a8b1242d92fb1b56081e1ddb86eba35411961252a103a1c083c2be6d"
		;;
esac
`, verityHashSize, snapPath))
	defer vscmd.Restore()

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	err = integrity.GenerateAndAppend(snapPath)
	c.Assert(err, IsNil)

	snapFile, err := os.Open(snapPath)
	c.Assert(err, IsNil)
	defer snapFile.Close()

	// check integrity header
	_, err = snapFile.Seek(orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	header := make([]byte, blockSize-1)
	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(blockSize)-1)

	var integrityDataHeader integrity.IntegrityDataHeader
	err = integrityDataHeader.Decode(header)
	c.Check(err, IsNil)
	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(verityHashSize+integrity.HeaderSize))
	c.Check(integrityDataHeader.DmVerity.RootHash, HasLen, 64)

	c.Assert(vscmd.Calls(), HasLen, 2)
	c.Check(vscmd.Calls()[0], DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], DeepEquals, []string{"veritysetup", "format", snapPath, snapPath + ".verity"})
}
