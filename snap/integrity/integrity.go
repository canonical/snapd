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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap/integrity/dm_verity"
)

const (
	blockSize = 4096
)

var (
	// magic is the magic prefix of snap metadata blocks.
	magic = []byte{'s', 'n', 'a', 'p'}
)

type Offset uint64

func (offset Offset) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%016x\"", offset)), nil
}

func (offset Offset) UnmarshalJSON(bytes []byte) error {
	val, err := strconv.ParseUint(string(bytes[1:len(bytes)-1]), 16, 64)
	if err != nil {
		return err
	}
	offset = Offset(val)
	return nil
}

// Read file size and align it to closest `blockSize`
func getAlignedFileSize(file *os.File) (uint64, error) {
	fi, err := file.Stat()
	if err != nil {
		return 0, err
	}

	logger.Debugf("File size: %d", fi.Size())
	if fi.Size() < 0 {
		return 0, fmt.Errorf("unexpected file size %d", fi.Size())
	}

	size := uint64(fi.Size())

	// Defensively align metadata block to 4KiB
	offsetNum := align(size) - size
	if offsetNum != 0 {
		logger.Debugf("SquashFS not aligned to 4KiB, adding %d bytes", offsetNum)
		size += offsetNum
	}

	return size, nil
}

// IntegrityMetadata gets appended first at the end of a squashfs packed snap
// before the dm-verity data
type IntegrityMetadata struct {
	Version          uint8                      `json:"version"`
	RootHash         string                     `json:"root-hash"`
	HashOffset       Offset                     `json:"hash-offset"`
	VeritySuperBlock dm_verity.VeritySuperBlock `json:"verity-superblock"`
}

func NewIntegrityMetadata(rootHash string, veritySuperBlock *dm_verity.VeritySuperBlock, snapFile *os.File) (*IntegrityMetadata, error) {
	metadata := IntegrityMetadata{}
	metadata.Version = 1
	metadata.RootHash = rootHash
	metadata.VeritySuperBlock = *veritySuperBlock

	snapFileSize, err := getAlignedFileSize(snapFile)
	if err != nil {
		return nil, err
	}

	// calculate HashOffset
	json, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	headerSize := align(uint64(len(magic) + len(json)))
	logger.Debugf("Magic size: %d", len(magic))
	logger.Debugf("Metadata JSON size: %d", len(json))
	logger.Debugf("Aligned header size: %d", headerSize)

	metadata.HashOffset = Offset(snapFileSize + headerSize)
	logger.Debugf("HashOffset (%d aligned): %d", blockSize, metadata.HashOffset)

	return &metadata, nil
}

// Align input `size` to closest `blockSize` value
func align(size uint64) uint64 {
	mod := size % blockSize
	if mod == 0 {
		return size
	}

	return size + (blockSize - mod)
}

func createHeader(metadata *IntegrityMetadata) ([]byte, error) {
	json, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	logger.Debugf("%s", string(json))

	// \0 terminate
	json = append(json, 0)

	headerSize := align(uint64(len(magic) + len(json)))
	header := make([]byte, headerSize)

	copy(header, append(magic, json...))

	return header, nil
}

func GenerateAndAppend(snapName string) (err error) {
	// Generate verity metadata
	hashFileName := snapName + ".verity"
	rootHash, sb, err := dm_verity.FormatNoSB(snapName, hashFileName)
	if err != nil {
		return err
	}

	snapFile, err := os.OpenFile(snapName, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer snapFile.Close()

	metadata, err := NewIntegrityMetadata(rootHash, sb, snapFile)
	if err != nil {
		return err
	}

	// Append header to snap
	header, err := createHeader(metadata)

	if _, err = snapFile.Write(header); err != nil {
		return err
	}

	// Append verity metadata to snap
	hashFile, err := os.Open(hashFileName)
	if err != nil {
		return err
	}
	defer func() {
		hashFile.Close()
		if e := os.Remove(hashFileName); e != nil {
			err = e
		}
	}()

	if _, err := io.Copy(snapFile, hashFile); err != nil {
		return err
	}

	return err
}
