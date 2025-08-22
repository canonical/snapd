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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/asserts"
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

func (params *IntegrityDataParams) crossCheck(vsb *dmverity.VeritySuperblock) error {

	// Check if the verity data that were found match the passed parameters
	alg := strings.ReplaceAll(string(vsb.Algorithm[:]), "\x00", "")
	if alg != params.HashAlg {
		return fmt.Errorf("unexpected algorithm: %s != %s", alg, params.HashAlg)
	}
	if vsb.DataBlockSize != uint32(params.DataBlockSize) {
		return fmt.Errorf("unexpected data block size: %d != %d", vsb.DataBlockSize, uint32(params.DataBlockSize))
	}
	if vsb.HashBlockSize != uint32(params.HashBlockSize) {
		return fmt.Errorf("unexpected hash block size: %d != %d", vsb.HashBlockSize, uint32(params.HashBlockSize))
	}

	encSalt := vsb.EncodedSalt()
	if encSalt != params.Salt {
		return fmt.Errorf("unexpected salt: %s != %s", vsb.EncodedSalt(), params.Salt)
	}

	return nil
}

// ErrNoIntegrityDataFoundInRevision is returned when a snap revision doesn't contain integrity data.
var ErrNoIntegrityDataFoundInRevision = errors.New("no integrity data found in revision")

// NewIntegrityDataParamsFromRevision will parse a revision for integrity data and return them as
// a new IntegrityDataParams object.
//
// An ErrNoIntegrityDataFoundInRevision error will be returned if there is no integrity data in the revision.
func NewIntegrityDataParamsFromRevision(rev *asserts.SnapRevision) (*IntegrityDataParams, error) {
	snapIntegrityData := rev.SnapIntegrityData()

	if len(snapIntegrityData) == 0 {
		return nil, ErrNoIntegrityDataFoundInRevision
	}

	// XXX: The first item in the snap-revision integrity data list is selected.
	// In future versions, extra logic will be required here to decide which integrity data
	// should be used based on extra information (i.e from the model).
	sid := snapIntegrityData[0]

	return &IntegrityDataParams{
		Type:          sid.Type,
		Version:       sid.Version,
		HashAlg:       sid.HashAlg,
		DataBlockSize: uint64(sid.DataBlockSize),
		HashBlockSize: uint64(sid.HashBlockSize),
		Digest:        sid.Digest,
		Salt:          sid.Salt,
		DataBlocks:    rev.SnapSize() / uint64(sid.DataBlockSize),
	}, nil
}

// ErrDmVerityDataNotFound is returned when dm-verity data for a snap are not found next to it.
var ErrDmVerityDataNotFound = errors.New("dm-verity data not found")

// ErrUnexpectedDmVerityData is returned when dm-verity data for a snap are available but don't match
// the parameters passed to LookupDmVerityDataAndCrossCheck.
var ErrUnexpectedDmVerityData = errors.New("unexpected dm-verity data")

// LookupDmVerityDataAndCrossCheck looks up dm-verity data for a snap based on its file name and validates
// that the superblock properties of the discovered dm-verity data match the passed parameters.
func LookupDmVerityDataAndCrossCheck(snapPath string, params *IntegrityDataParams) (string, error) {
	hashFileName := snapPath + ".verity"

	vsb, err := readDmVeritySuperblock(hashFileName)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %q doesn't exist.", ErrDmVerityDataNotFound, hashFileName)
	}

	if err != nil {
		return "", err
	}

	err = params.crossCheck(vsb)
	if err != nil {
		return "", fmt.Errorf("%w %q: %s", ErrUnexpectedDmVerityData, hashFileName, err.Error())
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
