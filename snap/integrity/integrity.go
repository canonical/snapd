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
	"bytes"
	"crypto"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/integrity/dmverity"
	"github.com/snapcore/snapd/snap/squashfs"
)

const (
	blockSize = 4096
	// For now that the header only includes a fixed-size string and a fixed-size hash,
	// the header size is always gonna be less than blockSize and will always get aligned
	// to blockSize.
	HeaderSize = 4096
	// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
	squashfsSuperblockBytesUsedOffset = 40
)

var (
	// magic is the magic prefix of snap extension blocks.
	magic = []byte{'s', 'n', 'a', 'p', 'e', 'x', 't'}
)

// align aligns input `size` to closest `blockSize` value
func align(size uint64) uint64 {
	return (size + blockSize - 1) / blockSize * blockSize
}

// An IntegrityData structure represents a snap's integrity data and
// contains information useful for locating it.
type IntegrityData struct {
	Header         *IntegrityDataHeader
	Offset         uint64
	SourceFilePath string
}

// FindIntegrityData returns integrity data information given a snap filename.
// It currently supports only integrity data attached at the end of snap files.
func FindIntegrityData(snapPath string) (*IntegrityData, error) {
	integrityData := IntegrityData{}

	snapFile, err := os.OpenFile(snapPath, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer snapFile.Close()

	snapFileInfo, err := snapFile.Stat()
	if err != nil {
		return nil, err
	}

	if !squashfs.FileHasSquashfsHeader(snapPath) {
		return nil, errors.New("input file does not contain a SquashFS filesystem")
	}

	// Seek to bytes_used field of SquashFS superblock
	_, err = snapFile.Seek(squashfsSuperblockBytesUsedOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	var squashFSSize uint64
	if err := binary.Read(snapFile, binary.LittleEndian, &squashFSSize); err != nil {
		return nil, err
	}

	logger.Debugf("SquashFS bytes used: %d", squashFSSize)

	// Align squashFSSize to blockSize
	offset := align(squashFSSize)

	if offset == uint64(snapFileInfo.Size()) {
		return nil, fmt.Errorf("integrity data not found for snap %s", snapPath)
	}

	integrityData.SourceFilePath = snapPath
	// TODO check for integrity data in separate file

	_, err = snapFile.Seek(int64(offset), io.SeekStart)
	if err != nil {
		return nil, err
	}

	integrityDataBytes := make([]byte, uint64(HeaderSize))
	_, err = io.ReadFull(snapFile, integrityDataBytes)
	if err != nil {
		return nil, fmt.Errorf("cannot read integrity data: %s", err)
	}

	integrityDataHeader, err := extractIntegrityDataHeader(integrityDataBytes)
	if err != nil {
		return nil, err
	}
	integrityData.Header = integrityDataHeader
	integrityData.Offset = offset

	return &integrityData, nil
}

// Validate checks integrity data against a snap-revision assertion.
// The entire integrity data including the header gets hashed and compared to the hash
// included in the integrity stanza in a snap-revision assertion.
func (integrityData IntegrityData) Validate(snapRev asserts.SnapRevision) error {
	integrityDataHash, err := integrityData.SHA3_384()
	if err != nil {
		return err
	}

	snapRevIntegrityData := snapRev.SnapIntegrity()
	if snapRevIntegrityData == nil {
		return errors.New("Snap revision assertion does not contain an integrity stanza")
	}

	if integrityDataHash != snapRevIntegrityData.SHA3_384 {
		return errors.New("integrity data hash mismatch")
	}
	return nil
}

// extractIntegrityDataHeader parses a byte array containing integrity data header information
// into an IntegrityDataHeader structure.
func extractIntegrityDataHeader(integrityDataBytes []byte) (*IntegrityDataHeader, error) {
	integrityDataHeader := IntegrityDataHeader{}

	err := integrityDataHeader.Decode(integrityDataBytes[:HeaderSize])
	if err != nil {
		return nil, err
	}

	return &integrityDataHeader, nil
}

// SHA3_384 computes the SHA3-384 digest of the integrity data and encodes it in the hash format used
// by snap assertions.
func (integrityData IntegrityData) SHA3_384() (string, error) {
	digest, _, err := osutil.PartialFileDigest(integrityData.SourceFilePath, crypto.SHA3_384, integrityData.Offset)
	if err != nil {
		return "", err
	}

	sha3_384, err := asserts.EncodeDigest(crypto.SHA3_384, digest)
	if err != nil {
		return "", fmt.Errorf("cannot encode snap's %q integrity data digest: %v", integrityData.SourceFilePath, err)
	}
	return sha3_384, nil
}

// IntegrityDataHeader gets appended first at the end of a squashfs packed snap
// before the dm-verity data. Size field includes the header size
type IntegrityDataHeader struct {
	Type     string        `json:"type"`
	Size     uint64        `json:"size,string"`
	DmVerity dmverity.Info `json:"dm-verity"`
}

// newIntegrityDataHeader constructs a new IntegrityDataHeader struct from a dmverity.Info struct.
func newIntegrityDataHeader(dmVerityBlock *dmverity.Info, integrityDataSize uint64) *IntegrityDataHeader {
	return &IntegrityDataHeader{
		Type:     "integrity",
		Size:     HeaderSize + integrityDataSize,
		DmVerity: *dmVerityBlock,
	}
}

// Encode serializes an IntegrityDataHeader struct to a null terminated json string.
func (integrityDataHeader IntegrityDataHeader) Encode() ([]byte, error) {
	jsonHeader, err := json.Marshal(integrityDataHeader)
	if err != nil {
		return nil, err
	}
	logger.Debugf("integrity data header:\n%s", string(jsonHeader))

	// \0 terminate
	jsonHeader = append(jsonHeader, 0)

	actualHeaderSize := align(uint64(len(magic) + len(jsonHeader) + 1))
	if actualHeaderSize > HeaderSize {
		return nil, errors.New("internal error: invalid integrity data header: wrong size")
	}

	header := make([]byte, HeaderSize)

	copy(header, append(magic, jsonHeader...))

	return header, nil
}

// Decode unserializes an null-terminated byte array containing JSON data to an
// IntegrityDataHeader struct.
func (integrityDataHeader *IntegrityDataHeader) Decode(input []byte) error {
	if !bytes.HasPrefix(input, magic) {
		return errors.New("invalid integrity data header: invalid magic value")
	}

	firstNull := bytes.IndexByte(input, '\x00')
	if firstNull == -1 {
		return errors.New("invalid integrity data header: no null byte found at end of input")
	}

	err := json.Unmarshal(input[len(magic):firstNull], &integrityDataHeader)
	if err != nil {
		return err
	}

	return nil
}

// GenerateAndAppend generates integrity data for a snap file and appends them
// to it.
// Integrity data are formed from a fixed-size header aligned to blockSize which
// includes the root hash followed by the generated dm-verity hash data.
func GenerateAndAppend(snapPath string) (err error) {
	// Generate verity metadata
	hashFileName := snapPath + ".verity"
	dmVerityBlock, err := dmverity.Format(snapPath, hashFileName)
	if err != nil {
		return err
	}

	hashFile, err := os.OpenFile(hashFileName, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		hashFile.Close()
		if e := os.Remove(hashFileName); e != nil {
			err = e
		}
	}()

	fi, err := hashFile.Stat()
	if err != nil {
		return err
	}

	integrityDataHeader := newIntegrityDataHeader(dmVerityBlock, uint64(fi.Size()))

	// Append header to snap
	header, err := integrityDataHeader.Encode()
	if err != nil {
		return err
	}

	snapFile, err := os.OpenFile(snapPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer snapFile.Close()

	if _, err = snapFile.Write(header); err != nil {
		return err
	}

	// Append verity metadata to snap
	if _, err := io.Copy(snapFile, hashFile); err != nil {
		return err
	}

	return err
}
