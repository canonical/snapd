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

	"github.com/snapcore/snapd/asserts"
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

const (
	zeroRootHash = "0000000000000000000000000000000000000000000000000000000000000000"
)

func (s *IntegrityTestSuite) TestIntegrityHeaderUnmarshalJSON(c *C) {
	headerSize := uint64(integrity.HeaderSize)
	var integrityDataHeader integrity.IntegrityDataHeader

	integrityDataHeaderJSON := `{
		"type": "integrity",
		"size": "` + fmt.Sprint(headerSize) + `",
		"dm-verity": {
			"root-hash": "` + zeroRootHash + `"
		}
	}`

	err := json.Unmarshal([]byte(integrityDataHeaderJSON), &integrityDataHeader)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, headerSize)
	c.Check(integrityDataHeader.DmVerity.RootHash, Equals, zeroRootHash)
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
	headerSize := uint64(integrity.HeaderSize)

	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	integrityDataHeaderJSON := `{
		"type": "integrity",
		"size": "` + fmt.Sprint(headerSize) + `",
		"dm-verity": {
			"root-hash": "` + zeroRootHash + `"
		}
	}`
	header := append(magic, integrityDataHeaderJSON...)
	header = append(header, 0)

	integrityDataHeaderBytes := make([]byte, headerSize)
	copy(integrityDataHeaderBytes, header)

	err := integrityDataHeader.Decode(integrityDataHeaderBytes)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, headerSize)
	c.Check(integrityDataHeader.DmVerity.RootHash, Equals, zeroRootHash)
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
	headerSize := uint64(integrity.HeaderSize)

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
		echo "VERITY header information for %[1]s.verity"
		echo "UUID:            	f8b4f201-fe4e-41a2-9f1d-4908d3c76632"
		echo "Hash type:       	1"
		echo "Data blocks:     	1"
		echo "Data block size: 	4096"
		echo "Hash block size: 	4096"
		echo "Hash algorithm:  	sha256"
		echo "Salt:            	f1a7f87b88692b388f47dbda4a3bdf790f5adc3104b325f8772aee593488bf15"
		echo "Root hash:      	e2926364a8b1242d92fb1b56081e1ddb86eba35411961252a103a1c083c2be6d"
		;;
esac
`, snapPath))
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

	header := make([]byte, headerSize)
	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(headerSize))

	var integrityDataHeader integrity.IntegrityDataHeader
	err = integrityDataHeader.Decode(header)
	c.Check(err, IsNil)
	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(2*headerSize))
	c.Check(integrityDataHeader.DmVerity.RootHash, HasLen, 64)

	c.Assert(vscmd.Calls(), HasLen, 2)
	c.Check(vscmd.Calls()[0], DeepEquals, []string{"veritysetup", "--version"})
	c.Check(vscmd.Calls()[1], DeepEquals, []string{"veritysetup", "format", snapPath, snapPath + ".verity"})
}

type testFindIntegrityDataData struct {
	snapPath  string
	orig_size uint64
}

func (s *IntegrityTestSuite) testFindIntegrityData(c *C, data *testFindIntegrityDataData) (*integrity.IntegrityDataHeader, error) {
	integrityData, err := integrity.FindIntegrityData(data.snapPath)
	if err != nil {
		return nil, err
	}

	// TODO: this will need to change when we add support for integrity data external to the snap
	c.Check(integrityData.SourceFilePath, Equals, data.snapPath)

	snapFile, err := os.Open(data.snapPath)
	if err != nil {
		return nil, err
	}
	defer snapFile.Close()

	// Read header from file
	header := make([]byte, integrity.HeaderSize)
	_, err = snapFile.Seek(int64(data.orig_size), io.SeekStart)
	c.Assert(err, IsNil)

	n, err := snapFile.Read(header)
	if err != nil {
		return nil, err
	}
	c.Assert(n, Equals, integrity.HeaderSize)

	var integrityDataHeader integrity.IntegrityDataHeader
	integrityDataHeader.Decode(header)

	return &integrityDataHeader, nil
}

func (s *IntegrityTestSuite) TestIntegrityDataAttached(c *C) {
	integrityDataHeaderBytes := snaptest.MockIntegrityDataHeaderBytes(c, zeroRootHash)
	snapPath, integrityData := snaptest.MakeTestSnapWithFilesAndIntegrityDataHeaderBytes(c, "name: foo\nversion: 1.0", nil, integrityDataHeaderBytes)

	integrityDataHeader, err := s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: integrityData.Offset,
	})

	c.Assert(err, IsNil)
	c.Check(integrityData.Header, DeepEquals, integrityDataHeader)
}

func (s *IntegrityTestSuite) TestSnapFileNotExist(c *C) {
	_, err := s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath: "foo.snap",
	})
	c.Check(err, ErrorMatches, "open foo.snap: no such file or directory")
}

func (s *IntegrityTestSuite) TestIntegrityDataNotAttached(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	_, err = s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: uint64(orig_size),
	})
	c.Check(err, ErrorMatches, "integrity data not found for snap "+snapPath)
}

func (s *IntegrityTestSuite) TestIntegrityDataAttachedWrongHeaderSmall(c *C) {

	smallHeader := make([]byte, uint64(integrity.BlockSize)-1)

	snapPath, _ := snaptest.MakeTestSnapWithFilesAndIntegrityDataHeaderBytes(c, "name: foo\nversion: 1.0", nil, smallHeader)

	_, err := s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath: snapPath,
	})
	c.Check(err, ErrorMatches, "cannot read integrity data: unexpected EOF")
}

func (s *IntegrityTestSuite) TestIntegrityDataAttachedWrongHeader(c *C) {

	wrongHeader := make([]byte, uint64(integrity.BlockSize))

	snapPath, integrityData := snaptest.MakeTestSnapWithFilesAndIntegrityDataHeaderBytes(c, "name: foo\nversion: 1.0", nil, wrongHeader)
	c.Assert(integrityData, IsNil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	size := snapFileInfo.Size()

	_, err = s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: uint64(size),
	})
	c.Check(err, ErrorMatches, "invalid integrity data header: invalid magic value")

}

func makeValidEncodedAssertion(extra string) string {
	return "type: snap-revision\n" +
		"authority-id: store-id1\n" +
		"snap-sha3-384: QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 123\n" +
		"snap-revision: 1\n" +
		extra +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		"timestamp: 2023-03-30T17:03:06Z\n" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

type testValidateIntegrityDataData struct {
	encodedIntegrityData string
}

func (s *IntegrityTestSuite) mockSnapRevFromTemplate(c *C, integrityData *integrity.IntegrityData, template string) *asserts.SnapRevision {
	blobSHA3_384, err := integrityData.SHA3_384()
	c.Assert(err, IsNil)

	encodedAssertion := strings.Replace(template, "XXX", blobSHA3_384, 1)

	a, err := asserts.Decode([]byte(makeValidEncodedAssertion(encodedAssertion)))
	c.Assert(err, IsNil)

	return a.(*asserts.SnapRevision)
}

func (s *IntegrityTestSuite) testValidateIntegrityData(c *C, data *testValidateIntegrityDataData) error {
	integrityDataHeaderBytes := snaptest.MockIntegrityDataHeaderBytes(c, zeroRootHash)
	_, integrityData := snaptest.MakeTestSnapWithFilesAndIntegrityDataHeaderBytes(c, "name: foo\nversion: 1.0", nil, integrityDataHeaderBytes)

	snapRev := s.mockSnapRevFromTemplate(c, integrityData, data.encodedIntegrityData)

	return integrityData.Validate(*snapRev)
}

func (s *IntegrityTestSuite) TestValidateIntegrityDataOk(c *C) {
	c.Check(s.testValidateIntegrityData(c, &testValidateIntegrityDataData{
		encodedIntegrityData: "integrity:\n" +
			"  sha3-384: XXX\n" +
			"  size: 128\n",
	}), IsNil)
}

func (s *IntegrityTestSuite) TestValidateIntegrityDataError(c *C) {
	c.Check(s.testValidateIntegrityData(c, &testValidateIntegrityDataData{
		encodedIntegrityData: "integrity:\n" +
			"  sha3-384: QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL\n" +
			"  size: 128\n",
	}), ErrorMatches, "integrity data hash mismatch")
}

func (s *IntegrityTestSuite) TestValidateIntegrityDataInvalidAssertionMissingStanza(c *C) {
	c.Check(s.testValidateIntegrityData(c, &testValidateIntegrityDataData{}), ErrorMatches, "Snap revision assertion does not contain an integrity stanza")
}
