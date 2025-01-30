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

// IntegrityDataParams struct includes all the parameters that are necessary
// to generate or lookup integrity data. Currently only data of type "dm-verity"
// are supported via the GenerateDmVerityData and LookupDmVerityData functions.
type IntegrityDataParams struct {
	// Type is the type of integrity data (Currently only "dm-verity" is supported).
	Type string
	// Version is the type-specific format type.
	Version uint
	// HashAlg is the hash algorithm used for integrity data.
	HashAlg string
	// DataBlocks is the number of data blocks on the data/target device. Blocks after
	// DataBlocks are inaccessible. This is not included in the assertion and is generated
	// by dividing the entire snap's size by the DataBlockSize field.
	DataBlocks uint64
	// DataBlockSize is the block size in bytes on a data/target device.
	DataBlockSize uint64
	// HashBlockSize is the size of a hash block in bytes.
	HashBlockSize uint64
	// Digest (for the dm-verity type) is the hash of the root hash block in
	// hexadecimanl encoding.
	Digest string
	// Salt is the salt value used during generation in hexadecimal encoding.
	Salt string
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
