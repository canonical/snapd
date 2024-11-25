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

package integrity

import (
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/snap/integrity/dmverity"
)

var (
	veritysetupFormat      = dmverity.Format
	readDmVeritySuperblock = dmverity.ReadSuperblock
)

type IntegrityDataParams struct {
	Type          string
	Version       uint
	HashAlg       string
	DataBlocks    uint64
	DataBlockSize uint64
	HashBlockSize uint64
	Digest        string
	Salt          string
}

// LookupDmVerityData looks up verity data for a snap and validates that they were generated
// using the input parameters.
func LookupDmVerityData(snapPath string, params *IntegrityDataParams) (string, error) {
	hashFileName := snapPath + ".verity"

	vsb, err := readDmVeritySuperblock(hashFileName)
	// if a verity data file doesn't exist simply return empty name
	if os.IsNotExist(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	err = vsb.Validate()
	if err != nil {
		return "", err
	}

	alg := strings.ReplaceAll(string(vsb.Algorithm[:]), "\x00", "")
	encSalt := vsb.EncodedSalt()

	// Check if the verity data that was found matches the passed parameters
	if alg != params.HashAlg {
		return "", fmt.Errorf("snap integrity: dm-verity data %q were generated with an unasserted algorithm: %s != %s",
			hashFileName, alg, params.HashAlg)
	}
	if vsb.DataBlockSize != uint32(params.DataBlockSize) {
		return "", fmt.Errorf("snap integrity: dm-verity data %q were generated with an unasserted data block size: %d != %d",
			hashFileName, vsb.DataBlockSize, uint32(params.DataBlockSize))
	}
	if vsb.HashBlockSize != uint32(params.HashBlockSize) {
		return "", fmt.Errorf("snap integrity: dm-verity data %q were generated with an unasserted hash block size: %d != %d",
			hashFileName, vsb.HashBlockSize, uint32(params.HashBlockSize))
	}
	if encSalt != params.Salt {
		return "", fmt.Errorf("snap integrity: dm-verity data %q were generated with an unasserted salt: %s != %s",
			hashFileName, vsb.EncodedSalt(), params.Salt)
	}

	return hashFileName, nil
}

// GenerateDmVerityData generates dm-verity data for a snap using the input parameters.
func GenerateDmVerityData(snapPath string, params *IntegrityDataParams) (string, string, error) {
	hashFileName := snapPath + ".verity"

	var opts = dmverity.DmVerityParams{
		Format:        uint8(dmverity.DefaultVerityFormat),
		Hash:          params.HashAlg,
		DataBlocks:    params.DataBlocks,
		DataBlockSize: params.DataBlockSize,
		HashBlockSize: params.HashBlockSize,
		Salt:          params.Salt,
	}

	rootHash, err := veritysetupFormat(snapPath, hashFileName, &opts)
	if err != nil {
		return "", "", err
	}

	return hashFileName, rootHash, nil
}
