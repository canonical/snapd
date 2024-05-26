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
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap/integrity/dmverity"
)

const (
	blockSize = 4096
	// For now that the header only includes a fixed-size string and a fixed-size hash,
	// the header size is always gonna be less than blockSize and will always get aligned
	// to blockSize.
	HeaderSize = 4096
)

// magic is the magic prefix of snap extension blocks.
var magic = []byte{'s', 'n', 'a', 'p', 'e', 'x', 't'}

// align aligns input `size` to closest `blockSize` value
func align(size uint64) uint64 {
	return (size + blockSize - 1) / blockSize * blockSize
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
	jsonHeader := mylog.Check2(json.Marshal(integrityDataHeader))

	logger.Debugf("integrity data header:\n%s", string(jsonHeader))

	// \0 terminate
	jsonHeader = append(jsonHeader, 0)

	actualHeaderSize := align(uint64(len(magic) + len(jsonHeader) + 1))
	if actualHeaderSize > HeaderSize {
		return nil, fmt.Errorf("internal error: invalid integrity data header: wrong size")
	}

	header := make([]byte, HeaderSize)

	copy(header, append(magic, jsonHeader...))

	return header, nil
}

// Decode unserializes an null-terminated byte array containing JSON data to an
// IntegrityDataHeader struct.
func (integrityDataHeader *IntegrityDataHeader) Decode(input []byte) error {
	if !bytes.HasPrefix(input, magic) {
		return fmt.Errorf("invalid integrity data header: invalid magic value")
	}

	firstNull := bytes.IndexByte(input, '\x00')
	if firstNull == -1 {
		return fmt.Errorf("invalid integrity data header: no null byte found at end of input")
	}
	mylog.Check(json.Unmarshal(input[len(magic):firstNull], &integrityDataHeader))

	return nil
}

// GenerateAndAppend generates integrity data for a snap file and appends them
// to it.
// Integrity data are formed from a fixed-size header aligned to blockSize which
// includes the root hash followed by the generated dm-verity hash data.
func GenerateAndAppend(snapPath string) (err error) {
	// Generate verity metadata
	hashFileName := snapPath + ".verity"
	dmVerityBlock := mylog.Check2(dmverity.Format(snapPath, hashFileName))

	hashFile := mylog.Check2(os.OpenFile(hashFileName, os.O_RDONLY, 0644))

	defer func() {
		hashFile.Close()
		if e := os.Remove(hashFileName); e != nil {
			err = e
		}
	}()

	fi := mylog.Check2(hashFile.Stat())

	integrityDataHeader := newIntegrityDataHeader(dmVerityBlock, uint64(fi.Size()))

	// Append header to snap
	header := mylog.Check2(integrityDataHeader.Encode())

	snapFile := mylog.Check2(os.OpenFile(snapPath, os.O_APPEND|os.O_WRONLY, 0644))

	defer snapFile.Close()
	mylog.Check2(snapFile.Write(header))
	mylog.Check2(

		// Append verity metadata to snap
		io.Copy(snapFile, hashFile))

	return err
}
