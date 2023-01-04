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
	"io"
	"os"

	"github.com/snapcore/snapd/snap/integrity/dm_verity"
)

const (
	blockSize  = 4096
	headerSize = 4096
)

var (
	// magic is the magic prefix of snap metadata blocks.
	magic = []byte{'s', 'n', 'a', 'p'}
)

// IntegrityHeader gets appended first at the end of a squashfs packed snap
// before the dm-verity data
type IntegrityHeader struct {
	Version    uint8
	RootHash   string
	HashOffset uint64
	VeritySB   dm_verity.VeritySuperBlock
}

func align(size uint64) uint64 {
	mod := size % blockSize
	if mod == 0 {
		return size
	}

	return size + (blockSize - mod)
}

func GenerateAndAppend(snapName string) error {

	// Generate verity metadata
	hashFileName := snapName + ".verity"
	rootHash, sb, err := dm_verity.FormatNoSB(snapName, hashFileName)
	if err != nil {
		return err
	}

	header := IntegrityHeader{}
	header.Version = 1
	header.RootHash = rootHash
	header.VeritySB = *sb

	// Open snap file
	snapFile, err := os.OpenFile(snapName, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer snapFile.Close()

	fsize, err := snapFile.Stat()
	if err != nil {
		return err
	}

	// TODO: Remove debug prints
	// fmt.Println("File size: ", fsize.Size())
	// fmt.Println("Magic size: ", len(magic))

	header.HashOffset = align(uint64(fsize.Size()) + uint64(len(magic)) + headerSize)

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return err
	}

	// TODO: Remove debug prints
	// fmt.Println(string(headerJSON))
	// fmt.Println("JSON string size: ", len(string(headerJSON)))

	serialized := append([]byte(headerJSON), 0)
	headerBytes := make([]byte, headerSize)
	copy(headerBytes, append(magic, serialized...))

	// Append header to snap
	if _, err = snapFile.Write(headerBytes); err != nil {
		return err
	}

	// Append verity metadata to snap
	hashFile, err := os.Open(hashFileName)

	_, err = io.Copy(snapFile, hashFile)
	if err != nil {
		return err
	}

	hashFile.Close()
	err = os.Remove(hashFileName)
	if err != nil {
		return err
	}

	return nil
}
