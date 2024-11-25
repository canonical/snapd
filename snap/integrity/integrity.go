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
	"os"
	"strings"

	"github.com/snapcore/snapd/snap/integrity/dmverity"
)

var (
	veritysetupFormat      = dmverity.Format
	readDmVeritySuperBlock = dmverity.ReadSuperBlock
)

type IntegrityData struct {
	Type          string
	Version       uint
	HashAlg       string
	DataBlocks    uint64
	DataBlockSize uint64
	HashBlockSize uint64
	Digest        string
	Salt          string
}

// GenerateDmVerityData generates dm-verity data for a snap.
//
// If verity data already exist, the verity superblock is examined to verify that it was
// generated using the passed parameters. The generation is then skipped.
//
// If verity data do not exist, they are generated using the passed parameters.
func GenerateDmVerityData(snapPath string, params *IntegrityData) (string, string, error) {
	hashFileName := snapPath + ".verity"

	vsb, err := readDmVeritySuperBlock(hashFileName)
	// if verity file doesn't exist, continue in order to generate it
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}

	if vsb != nil {
		// Consistency check
		err := vsb.Validate()
		if err != nil {
			return "", "", err
		}

		alg := strings.ReplaceAll(string(vsb.Algorithm[:]), "\x00", "")
		salt := string(vsb.Salt[:])

		// Check if the verity data that was found matches the passed parameters
		if (alg == params.HashAlg) &&
			(vsb.Data_block_size == uint32(params.DataBlockSize)) &&
			(vsb.Hash_block_size == uint32(params.HashBlockSize)) &&
			(salt != params.Salt) {
			// return empty root hash since we didn't generate it
			return hashFileName, "", nil
		}
	}

	var opts = dmverity.DmVerityParams{
		Format:        uint8(dmverity.DefaultVerityFormat),
		Hash:          params.HashAlg,
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
