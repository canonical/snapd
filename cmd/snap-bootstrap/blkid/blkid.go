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

package blkid

//#cgo CFLAGS: -D_FILE_OFFSET_BITS=64
//#cgo pkg-config: blkid
//#cgo LDFLAGS:
//
//#include <stdlib.h>
//#include <blkid.h>
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	BLKID_PARTS_ENTRY_DETAILS int = C.BLKID_PARTS_ENTRY_DETAILS
)

type AbstractBlkidProbe interface {
	LookupValue(entryName string) (string, error)
	Close()
	EnablePartitions(value bool)
	EnableSuperblocks(value bool)
	SetPartitionsFlags(flags int)
	DoSafeprobe() error
	GetPartitions() (AbstractBlkidPartlist, error)
}

type AbstractBlkidPartlist interface {
	GetPartitions() []AbstractBlkidPartition
}

type AbstractBlkidPartition interface {
	GetName() string
	GetUUID() string
}

type BlkidProbe struct {
	probeHandle C.blkid_probe
}

func newProbeFromFilenameImpl(node string) (AbstractBlkidProbe, error) {
	cnode := C.CString(node)
	defer C.free(unsafe.Pointer(cnode))
	C.blkid_new_probe_from_filename(cnode)
	probe, err := C.blkid_new_probe_from_filename(cnode)
	if probe == nil {
		return nil, err
	}
	return &BlkidProbe{probe}, nil
}

var NewProbeFromFilename = newProbeFromFilenameImpl

func (p *BlkidProbe) LookupValue(entryName string) (string, error) {
	var value *C.char
	var value_len C.size_t
	cname := C.CString(entryName)
	defer C.free(unsafe.Pointer(cname))
	res := C.blkid_probe_lookup_value(p.probeHandle, cname, &value, &value_len)
	if res < 0 {
		return "", fmt.Errorf("Probe value was not found: %s", entryName)
	}
	if value_len > 0 {
		return C.GoStringN(value, C.int(value_len-1)), nil
	} else {
		return "", fmt.Errorf("Probe value has unexpected size")
	}
}

func (p *BlkidProbe) Close() {
	C.blkid_free_probe(p.probeHandle)
}

func (p *BlkidProbe) EnablePartitions(value bool) {
	v := 0
	if value {
		v = 1
	}
	C.blkid_probe_enable_partitions(p.probeHandle, C.int(v))
}

func (p *BlkidProbe) EnableSuperblocks(value bool) {
	v := 0
	if value {
		v = 1
	}
	C.blkid_probe_enable_superblocks(p.probeHandle, C.int(v))
}

func (p *BlkidProbe) SetPartitionsFlags(flags int) {
	C.blkid_probe_set_partitions_flags(p.probeHandle, C.int(flags))
}

func (p *BlkidProbe) DoSafeprobe() error {
	res, err := C.blkid_do_safeprobe(p.probeHandle)
	if res < 0 {
		return err
	}
	return nil
}

type BlkidPartlist struct {
	partlistHandle C.blkid_partlist
}

func (p *BlkidProbe) GetPartitions() (AbstractBlkidPartlist, error) {
	partitions, err := C.blkid_probe_get_partitions(p.probeHandle)
	if partitions == nil {
		return nil, err
	}
	return &BlkidPartlist{partitions}, nil
}

type BlkidPartition struct {
	partitionHandle C.blkid_partition
}

func (p *BlkidPartlist) GetPartitions() []AbstractBlkidPartition {
	npartitions := C.blkid_partlist_numof_partitions(p.partlistHandle)
	ret := make([]AbstractBlkidPartition, npartitions)
	for i := 0; i < int(npartitions); i++ {
		partition := C.blkid_partlist_get_partition(p.partlistHandle, C.int(i))
		ret[i] = &BlkidPartition{partition}
	}
	return ret
}

func (p *BlkidPartition) GetName() string {
	return C.GoString(C.blkid_partition_get_name(p.partitionHandle))
}

func (p *BlkidPartition) GetUUID() string {
	return C.GoString(C.blkid_partition_get_uuid(p.partitionHandle))
}
