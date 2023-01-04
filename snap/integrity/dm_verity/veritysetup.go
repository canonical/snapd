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

package dm_verity

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

type VeritySuperBlock struct {
	Version        uint64
	UUID           string
	HashType       uint64
	DataBlocks     uint64
	DataBlockSize  uint64
	HashBlocks     uint64
	HashBlockSize  uint64
	HashAlgorithm  string
	Salt           string
	HashDeviceSize uint64
}

func NewVeritySuperBlock() VeritySuperBlock {
	sb := VeritySuperBlock{}
	sb.Version = 1
	return sb
}

func parseVeritySetupOutput(output []byte) (string, *VeritySuperBlock) {
	sb := NewVeritySuperBlock()
	rootHash := ""

	for _, line := range strings.Split(string(output), "\n") {
		cols := strings.Split(line, ":")
		if len(cols) != 2 {
			continue
		}

		key := strings.TrimSpace(cols[0])
		val := strings.TrimSpace(cols[1])
		switch key {
		case "UUID":
			sb.UUID = val
		case "Hash type":
			sb.HashType, _ = strconv.ParseUint(val, 10, 64)
		case "Data blocks":
			sb.DataBlocks, _ = strconv.ParseUint(val, 10, 64)
		case "Data block size":
			sb.DataBlockSize, _ = strconv.ParseUint(val, 10, 64)
		case "Hash blocks":
			sb.HashBlocks, _ = strconv.ParseUint(val, 10, 64)
		case "Hash block size":
			sb.HashBlockSize, _ = strconv.ParseUint(val, 10, 64)
		case "Hash algorithm":
			sb.HashAlgorithm = val
		case "Salt":
			sb.Salt = val
		case "Hash device size":
			numBytes := strings.TrimSpace(strings.Split(val, "[")[0])
			sb.HashDeviceSize, _ = strconv.ParseUint(numBytes, 10, 64)
		case "Root hash":
			rootHash = val
		}
	}

	return rootHash, &sb

}

// Returns superblock information for inclusion in the integrity header instead
// of storing it in the beginning of the hash device
func FormatNoSB(dataDevice string, hashDevice string) (string, *VeritySuperBlock, error) {
	cmd := exec.Command("veritysetup", "format", "--no-superblock", dataDevice, hashDevice)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, osutil.OutputErr(output, err)
	}

	// TODO: Remove debug print
	// fmt.Println(string(output))

	rootHash, sb := parseVeritySetupOutput(output)

	return rootHash, sb, nil
}
