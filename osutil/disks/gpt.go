// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package disks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

type GPTLBA uint64
type GPTGUID [16]byte

// https://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_table_header_(LBA_1)
type GPTHeader struct {
	Signature      [8]byte
	Revision       uint32
	HeaderSize     uint32
	HeaderCRC      uint32
	Reserved       uint32
	CurrentLBA     GPTLBA
	AlternateLBA   GPTLBA
	FirstUsableLBA GPTLBA
	LastUsableLBA  GPTLBA
	DiskGUID       [16]byte
	EntriesLBA     GPTLBA
	NEntries       uint32
	EntrySize      uint32
	EntriesCRC     uint32
}

func verifyHeader(rawHeader []byte, header GPTHeader) error {
	if !bytes.Equal(header.Signature[:], []byte("EFI PART")) {
		return fmt.Errorf("GPT Header does not start with the magic string")
	}

	if header.Revision != 1<<16 {
		return fmt.Errorf("GPT header revision is not 1.0")
	}

	if int(header.HeaderSize) < binary.Size(header) {
		return fmt.Errorf("GPT header size is smaller than the minimum valid size")
	}

	if int(header.HeaderSize) > len(rawHeader) {
		return fmt.Errorf("GPT header size is larger than the maximum supported size")
	}

	// To calculate the proper CRC, we need to reset the value
	// of the CRC in the header to 0.
	for i := 0; i < 4; i++ {
		rawHeader[8+4+4+i] = 0
	}
	crc := crc32.ChecksumIEEE(rawHeader[:header.HeaderSize])
	if crc != header.HeaderCRC {
		return fmt.Errorf("GPT header CRC32 checksum failed: %v != %v", crc, header.HeaderCRC)
	}

	return nil
}

func LoadGPTHeader(devfd *os.File) (GPTHeader, error) {
	var header GPTHeader
	rawHeader := make([]byte, 512)
	read, err := devfd.Read(rawHeader)
	if err != nil {
		return GPTHeader{}, err
	}

	if read < binary.Size(header) {
		return GPTHeader{}, fmt.Errorf("Not enough data was read")
	}

	buf := bytes.NewReader(rawHeader[:read])
	binary.Read(buf, binary.LittleEndian, &header)

	if err := verifyHeader(rawHeader[:read], header); err != nil {
		return GPTHeader{}, err
	}

	return header, nil
}

func ReadGPTHeader(device string) (GPTHeader, error) {
	devfd, err := os.Open(device)
	if err != nil {
		return GPTHeader{}, err
	}
	defer devfd.Close()

	if _, err := devfd.Seek(512, os.SEEK_SET); err != nil {
		return GPTHeader{}, err
	}

	header, main_err := LoadGPTHeader(devfd)
	if main_err != nil {
		// Read the backup header
		_, err := devfd.Seek(-512, os.SEEK_END)
		if err != nil {
			return GPTHeader{}, main_err
		}

		header, err = LoadGPTHeader(devfd)
		if err != nil {
			return GPTHeader{}, main_err
		}
	}

	return header, nil
}

func CalculateLastUsableLBA(device string) (uint64, error) {
	header, err := ReadGPTHeader(device)
	if err != nil {
		return 0, err
	}
	sectors, err := blockDeviceSizeInSectors(device)
	if err != nil {
		return 0, err
	}
	tableSize := uint64(header.NEntries) * uint64(header.EntrySize)
	// Rounded up division for number of sectors
	tableSizeInSectors := (tableSize + 511) / 512

	//	|                 |
	//	| Last Usable LBA |  sectors - tableSizeInSectors - 2
	//	+-----------------+
	//	| Backup Table    |  sectors - 1 - tableSizeInSectors
	//	|                 |
	//	+-----------------+
	//	| Backup Header   |  sectors - 1
	//	+-----------------+
	//	  End of disk        sectors

	return sectors - tableSizeInSectors - 2, nil
}
