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
	"os"
	"testing"

	. "gopkg.in/check.v1"

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

	restore := integrity.MockVeritysetupFormat(func(dataDevice string, hashDevice string, inputParams *dmverity.DmVerityParams) (string, error) {
		c.Assert(dataDevice, Equals, snapPath)
		c.Assert(inputParams.Format, Equals, uint8(dmverity.DefaultVerityFormat))
		if params.integrityDataParams != nil {
			c.Assert(inputParams.Hash, Equals, params.integrityDataParams.HashAlg)
			c.Assert(inputParams.DataBlockSize, Equals, params.integrityDataParams.DataBlockSize)
			c.Assert(inputParams.HashBlockSize, Equals, params.integrityDataParams.HashBlockSize)
			c.Assert(inputParams.Salt, Equals, params.integrityDataParams.Salt)
		}
		rootHash := "e2926364a8b1242d92fb1b56081e1ddb86eba35411961252a103a1c083c2be6d"
		return rootHash, nil
	})
	defer restore()

	hashFileName, rootHash, err := integrity.GenerateDmVerityData(snapPath, params.integrityDataParams)
	c.Check(err, IsNil)
	c.Check(hashFileName, Equals, snapPath+".verity")
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

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return &sb, nil
	})
	defer restore()

	integrityDataParams := integrity.IntegrityDataParams{
		HashAlg:       "sha256",
		DataBlockSize: 4096,
		HashBlockSize: 4096,
		Salt:          sb.EncodedSalt(),
	}

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &integrityDataParams)
	c.Assert(err, IsNil)
	c.Check(hashFileName, Equals, snapPath+".verity")
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataCrossCheckError(c *C) {
	snapPath := "foo.snap"

	// sb, _ := dmverity.ReadSuperBlockFromFile("testdata/testdisk.verity")
	// sbJson, _ := json.Marshal(sb)
	sbJson := `{"version":1,"hashType":1,"uuid":[147,116,13,94,144,57,74,7,146,25,189,53,88,130,182,75],"algorithm":[115,104,97,50,53,54,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"dataBlockSize":4096,"hashBlockSize":4096,"dataBlocks":2048,"saltSize":32,"salt":[70,174,227,175,251,208,69,86,35,233,7,187,127,198,34,153,155,172,76,134,250,38,56,8,172,21,36,11,22,40,100,88,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}`
	var sb dmverity.VeritySuperblock
	err := json.Unmarshal([]byte(sbJson), &sb)
	c.Assert(err, IsNil)

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return &sb, nil
	})
	defer restore()

	errMsg := "unexpected dm-verity data \"foo.snap.verity\": "
	tests := []struct {
		idp         integrity.IntegrityDataParams
		expectedErr string
	}{
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "foo",
				DataBlockSize: 4096,
				HashBlockSize: 4096,
				Salt:          sb.EncodedSalt(),
			},
			expectedErr: errMsg + "unexpected algorithm: sha256 != foo",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 1,
				HashBlockSize: 4096,
				Salt:          sb.EncodedSalt(),
			},
			expectedErr: errMsg + "unexpected data block size: 4096 != 1",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 4096,
				HashBlockSize: 1,
				Salt:          sb.EncodedSalt(),
			},
			expectedErr: errMsg + "unexpected hash block size: 4096 != 1",
		},
		{
			idp: integrity.IntegrityDataParams{
				HashAlg:       "sha256",
				DataBlockSize: 4096,
				HashBlockSize: 4096,
				Salt:          "salt",
			},
			expectedErr: errMsg + "unexpected salt: " + sb.EncodedSalt() + " != salt",
		},
	}

	for _, t := range tests {
		hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, &t.idp)
		c.Assert(hashFileName, Equals, "")
		c.Check(errors.Is(err, integrity.ErrUnexpectedDmVerityData), Equals, true)
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataNotExist(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return nil, os.ErrNotExist
	})
	defer restore()

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, nil)
	c.Check(hashFileName, Equals, "")
	c.Check(errors.Is(err, integrity.ErrDmVerityDataNotFound), Equals, true)
	c.Check(err, ErrorMatches, "dm-verity data not found: \"foo.snap.verity\" doesn't exist.")
}

func (s *IntegrityTestSuite) TestLookupDmVerityDataAnyError(c *C) {
	snapPath := "foo.snap"

	restore := integrity.MockReadDmVeritySuperblock(func(filename string) (*dmverity.VeritySuperblock, error) {
		return nil, errors.New("any other error")
	})
	defer restore()

	hashFileName, err := integrity.LookupDmVerityDataAndCrossCheck(snapPath, nil)
	c.Check(hashFileName, Equals, "")
	c.Check(err, ErrorMatches, "any other error")
}
