// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/snap/integrity/dmverity"
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

type generateDmVerityDataParams struct {
	integrityDataParams *integrity.IntegrityDataParams
	expectedRootHash    string
}

func (s *IntegrityTestSuite) testGenerateDmVerityData(c *C, params *generateDmVerityDataParams) {
	snapPath := "foo.snap"
	rootHash := "e2926364a8b1242d92fb1b56081e1ddb86eba35411961252a103a1c083c2be6d"

	restore := integrity.MockVeritysetupFormat(func(dataDevice string, hashDevice string, inputParams *dmverity.DmVerityParams) (string, error) {
		c.Assert(dataDevice, Equals, snapPath)
		c.Assert(inputParams.Format, Equals, uint8(dmverity.DefaultVerityFormat))
		if params.integrityDataParams != nil {
			c.Assert(inputParams.Hash, Equals, params.integrityDataParams.HashAlg)
			c.Assert(inputParams.DataBlockSize, Equals, params.integrityDataParams.DataBlockSize)
			c.Assert(inputParams.HashBlockSize, Equals, params.integrityDataParams.HashBlockSize)
			c.Assert(inputParams.Salt, Equals, params.integrityDataParams.Salt)
		}
		return rootHash, nil
	})
	defer restore()

	restore = integrity.MockOsRename(func(src, dst string) error {
		c.Assert(src, Equals, snapPath+".dmverity")
		c.Assert(dst, Equals, snapPath+".dmverity_"+rootHash)
		return nil
	})
	defer restore()

	hashFileName, rootHash, err := integrity.GenerateDmVerityData(snapPath, params.integrityDataParams)
	c.Check(err, IsNil)
	c.Check(hashFileName, Equals, snapPath+".dmverity_"+rootHash)
	c.Check(rootHash, Equals, params.expectedRootHash)
}

func (s *IntegrityTestSuite) TestGenerateDmVerityDataSuccess(c *C) {
	params := &generateDmVerityDataParams{
		integrityDataParams: &integrity.IntegrityDataParams{
			HashAlg:       "alg",
			DataBlockSize: 1000,
			HashBlockSize: 1000,
			Salt:          "salt",
		},
		expectedRootHash: "e2926364a8b1242d92fb1b56081e1ddb86eba35411961252a103a1c083c2be6d",
	}

	s.testGenerateDmVerityData(c, params)
}

func (s *IntegrityTestSuite) TestGenerateDmVerityDataVeritySetupError(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockVeritysetupFormat(func(dataDevice string, hashDevice string, inputParams *dmverity.DmVerityParams) (string, error) {
		return "", errors.New("veritysetup error")
	})
	defer restore()

	id := &integrity.IntegrityDataParams{}

	hashFileName, rootHash, err := integrity.GenerateDmVerityData(snapPath, id)
	c.Check(hashFileName, Equals, "")
	c.Check(rootHash, Equals, "")
	c.Check(err, ErrorMatches, "veritysetup error")
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataSuccess(c *C) {
	snapPath := "foo.snap"

	// sb, _ := dmverity.ReadSuperBlockFromFile("testdata/testdisk.verity")
	// sbJson, _ := json.Marshal(sb)
	sbJson := `{"version":1,"hashType":1,"uuid":[147,116,13,94,144,57,74,7,146,25,189,53,88,130,182,75],"algorithm":[115,104,97,50,53,54,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"dataBlockSize":4096,"hashBlockSize":4096,"dataBlocks":2048,"saltSize":32,"salt":[70,174,227,175,251,208,69,86,35,233,7,187,127,198,34,153,155,172,76,134,250,38,56,8,172,21,36,11,22,40,100,88,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}`
	var sb dmverity.VeritySuperblock
	err := json.Unmarshal([]byte(sbJson), &sb)
	c.Assert(err, IsNil)

	digest := "test"
	verityFilePath := snapPath + ".dmverity_" + digest

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		c.Assert(filename, Equals, verityFilePath)
		return &sb, nil
	})
	defer restore()

	integrityDataParams := integrity.IntegrityDataParams{
		HashAlg:       "sha256",
		DataBlockSize: 4096,
		HashBlockSize: 4096,
		Salt:          sb.EncodedSalt(),
		Digest:        digest,
	}

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &integrityDataParams)
	c.Assert(err, IsNil)
	c.Check(hashFileName, Equals, verityFilePath)
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataCrossCheckError(c *C) {
	snapPath := "foo.snap"

	// sb, _ := dmverity.ReadSuperBlockFromFile("testdata/testdisk.verity")
	// sbJson, _ := json.Marshal(sb)
	sbJson := `{"version":1,"hashType":1,"uuid":[147,116,13,94,144,57,74,7,146,25,189,53,88,130,182,75],"algorithm":[115,104,97,50,53,54,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"dataBlockSize":4096,"hashBlockSize":4096,"dataBlocks":2048,"saltSize":32,"salt":[70,174,227,175,251,208,69,86,35,233,7,187,127,198,34,153,155,172,76,134,250,38,56,8,172,21,36,11,22,40,100,88,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}`
	var sb dmverity.VeritySuperblock
	err := json.Unmarshal([]byte(sbJson), &sb)
	c.Assert(err, IsNil)

	digest := "test"
	verityFilePath := snapPath + ".dmverity_" + digest

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		c.Assert(filename, Equals, verityFilePath)
		return &sb, nil
	})
	defer restore()

	errMsg := fmt.Sprintf("unexpected dm-verity data %q: ", verityFilePath)
	tests := []struct {
		idp         integrity.IntegrityDataParams
		expectedErr string
		comment     string
	}{
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "foo",
				DataBlockSize: 4096,
				HashBlockSize: 4096,
				Salt:          sb.EncodedSalt(),
				Digest:        digest,
			},
			expectedErr: errMsg + "unexpected algorithm: sha256 != foo",
			comment:     "error when algorithm doesn't match",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 1,
				HashBlockSize: 4096,
				Salt:          sb.EncodedSalt(),
				Digest:        digest,
			},
			expectedErr: errMsg + "unexpected data block size: 4096 != 1",
			comment:     "error when block size doesn't match",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 4096,
				HashBlockSize: 1,
				Salt:          sb.EncodedSalt(),
				Digest:        digest,
			},
			expectedErr: errMsg + "unexpected hash block size: 4096 != 1",
			comment:     "error when hash block size doesn't match",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 4096,
				HashBlockSize: 4096,
				Salt:          "salt",
				Digest:        digest,
			},
			expectedErr: errMsg + "unexpected salt: " + sb.EncodedSalt() + " != salt",
			comment:     "error when salt doesn't match",
		},
	}

	for _, t := range tests {
		hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &t.idp)
		c.Assert(hashFileName, Equals, "", Commentf(t.comment))
		c.Check(errors.Is(err, integrity.ErrUnexpectedDmVerityData), Equals, true, Commentf(t.comment))
		c.Check(err, ErrorMatches, t.expectedErr, Commentf(t.comment))
	}
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataNilParams(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return nil, os.ErrNotExist
	})
	defer restore()

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, nil)
	c.Check(hashFileName, Equals, "")
	c.Check(errors.Is(err, integrity.ErrIntegrityDataParamsNotFound), Equals, true)
	c.Check(err, ErrorMatches, "integrity data parameters not found")
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataNotExist(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return nil, os.ErrNotExist
	})
	defer restore()

	integrityDataParams := integrity.IntegrityDataParams{
		HashAlg:       "sha256",
		DataBlockSize: 4096,
		HashBlockSize: 4096,
	}

	digest := ""
	verityFilePath := snapPath + ".dmverity_" + digest

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &integrityDataParams)
	c.Check(hashFileName, Equals, "")
	c.Check(errors.Is(err, integrity.ErrDmVerityDataNotFound), Equals, true)
	c.Check(err, ErrorMatches, fmt.Sprintf("dm-verity data not found: %q doesn't exist.", verityFilePath))
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataAnyError(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return nil, errors.New("any other error")
	})
	defer restore()

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &integrity.IntegrityDataParams{})
	c.Check(hashFileName, Equals, "")
	c.Check(err, ErrorMatches, "any other error")
}

func makeMockSnapRevisionAssertion(c *C, integrityData string) *asserts.SnapRevision {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ts := time.Now().Truncate(time.Second).UTC()
	tsLine := "timestamp: " + ts.Format(time.RFC3339) + "\n"

	assertsString := "type: snap-revision\n" +
		"authority-id: store-id1\n" +
		"snap-sha3-384: " + hash + "\n" +
		"snap-id: snap-id-1\n" +
		"snap-size: 123\n" +
		"snap-revision: 1\n" +
		integrityData +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	a, err := asserts.Decode([]byte(assertsString))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	snapRev := a.(*asserts.SnapRevision)

	return snapRev
}

func (s *IntegrityTestSuite) TestNewIntegrityDataParamsFromRevision(c *C) {
	verity_hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	verity_salt := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	integrityData := "integrity:\n" +
		"  -\n" +
		"    type: dm-verity\n" +
		"    digest: " + verity_hash + "\n" +
		"    version: 1\n" +
		"    hash-algorithm: sha256\n" +
		"    data-block-size: 4096\n" +
		"    hash-block-size: 4096\n" +
		"    salt: " + verity_salt + "\n"
	rev := makeMockSnapRevisionAssertion(c, integrityData)

	expectedParams := &integrity.IntegrityDataParams{
		Type:          "dm-verity",
		Version:       0x1,
		HashAlg:       "sha256",
		DataBlocks:    0x0,
		DataBlockSize: 0x1000,
		HashBlockSize: 0x1000,
		Digest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Salt:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	params, err := integrity.NewIntegrityDataParamsFromRevision(rev)
	c.Check(err, IsNil)
	c.Check(params, DeepEquals, expectedParams)
}

func (s *IntegrityTestSuite) TestNewIntegrityDataParamsFromRevisionNotFound(c *C) {
	rev := makeMockSnapRevisionAssertion(c, "")
	_, err := integrity.NewIntegrityDataParamsFromRevision(rev)
	c.Check(err, Equals, integrity.ErrNoIntegrityDataFoundInRevision)
}
