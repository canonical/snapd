// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

	BLKID_SUBLKS_LABEL int = C.BLKID_SUBLKS_LABEL
)

// AbstractBlkidProbe is wrapper for blkid_probe
// See "Low-level" section of libblkid documentation
type AbstractBlkidProbe interface {
	// LookupValue is a wrapper for blkid_probe_lookup_value
	LookupValue(entryName string) (string, error)
	// Close is a wrapper for blkid_free_probe
	Close()
	// EnablePartitions is a wrapper for blkid_probe_enable_partitions
	EnablePartitions(value bool)
	// SetPartitionsFlags is a wrapper for blkid_probe_set_partitions_flags
	SetPartitionsFlags(flags int)
	// EnableSuperblocks is a wrapper for blkid_probe_enable_superblocks
	EnableSuperblocks(value bool)
	// SetSuperblockFlags is a wrapper for blkid_probe_set_superblocks_flags
	SetSuperblockFlags(flags int)
	// DoSafeprobe is a wrapper for blkid_do_safeprobe
	DoSafeprobe() error
	// GetPartitions is a wrapper for blkid_probe_get_partitions
	GetPartitions() (AbstractBlkidPartlist, error)
}

// AbstractBlkidPartlist is a wrapper for blkid_partlist
type AbstractBlkidPartlist interface {
	// GetPartitions is a wrapper for blkid_partlist_get_partition
	// and blkid_partlist_numof_partitions.
	GetPartitions() []AbstractBlkidPartition
}

// AbstractBlkidPartition is a wrapper for blkid_partition
type AbstractBlkidPartition interface {
	// GetName is a wrapper for blkid_partition_get_name
	GetName() string
	// GetUUID is a wrapper for blkid_partition_get_uuid
	GetUUID() string
	// GetPartNo is a wrapper for blkid_partition_get_partno
	GetPartNo() int
}

type blkidProbe struct {
	probeHandle C.blkid_probe
}

func newProbeFromFilenameImpl(node string) (AbstractBlkidProbe, error) {
	cnode := C.CString(node)
	defer C.free(unsafe.Pointer(cnode))
	probe, err := C.blkid_new_probe_from_filename(cnode)
	if probe == nil {
		if err == nil {
			return nil, fmt.Errorf("blkid_new_probe_from_filename failed but no error was returned")
		}
		return nil, err
	}
	return &blkidProbe{probe}, nil
}

var NewProbeFromFilename = newProbeFromFilenameImpl

func (p *blkidProbe) checkProbe() {
	if p.probeHandle == nil {
		panic("used blkid probe after Close")
	}
}

func (p *blkidProbe) LookupValue(entryName string) (string, error) {
	p.checkProbe()
	var value *C.char
	var value_len C.size_t
	cname := C.CString(entryName)
	defer C.free(unsafe.Pointer(cname))
	res := C.blkid_probe_lookup_value(p.probeHandle, cname, &value, &value_len)
	if res < 0 {
		return "", fmt.Errorf("probe value was not found: %s", entryName)
	}
	if value_len > 0 {
		return C.GoStringN(value, C.int(value_len-1)), nil
	} else {
		return "", fmt.Errorf("probe value has unexpected size")
	}
}

func (p *blkidProbe) Close() {
	p.checkProbe()
	C.blkid_free_probe(p.probeHandle)
	p.probeHandle = C.blkid_probe(nil)
}

func (p *blkidProbe) EnablePartitions(value bool) {
	p.checkProbe()
	v := 0
	if value {
		v = 1
	}
	C.blkid_probe_enable_partitions(p.probeHandle, C.int(v))
}

func (p *blkidProbe) SetPartitionsFlags(flags int) {
	p.checkProbe()
	C.blkid_probe_set_partitions_flags(p.probeHandle, C.int(flags))
}

func (p *blkidProbe) EnableSuperblocks(value bool) {
	p.checkProbe()
	v := 0
	if value {
		v = 1
	}
	C.blkid_probe_enable_superblocks(p.probeHandle, C.int(v))
}

func (p *blkidProbe) SetSuperblockFlags(flags int) {
	p.checkProbe()
	C.blkid_probe_set_superblocks_flags(p.probeHandle, C.int(flags))
}

func (p *blkidProbe) DoSafeprobe() error {
	p.checkProbe()
	res, err := C.blkid_do_safeprobe(p.probeHandle)
	if res < 0 {
		return err
	}
	return nil
}

type blkidPartlist struct {
	partlistHandle C.blkid_partlist
}

func (p *blkidProbe) GetPartitions() (AbstractBlkidPartlist, error) {
	p.checkProbe()
	partitions, err := C.blkid_probe_get_partitions(p.probeHandle)
	if partitions == nil {
		return nil, err
	}
	return &blkidPartlist{partitions}, nil
}

type blkidPartition struct {
	partitionHandle C.blkid_partition
}

func (p *blkidPartlist) GetPartitions() []AbstractBlkidPartition {
	npartitions := C.blkid_partlist_numof_partitions(p.partlistHandle)
	ret := make([]AbstractBlkidPartition, npartitions)
	for i := 0; i < int(npartitions); i++ {
		partition := C.blkid_partlist_get_partition(p.partlistHandle, C.int(i))
		ret[i] = &blkidPartition{partition}
	}
	return ret
}

func (p *blkidPartition) GetName() string {
	return C.GoString(C.blkid_partition_get_name(p.partitionHandle))
}

func (p *blkidPartition) GetUUID() string {
	return C.GoString(C.blkid_partition_get_uuid(p.partitionHandle))
}

func (p *blkidPartition) GetPartNo() int {
	return int((C.int)(C.blkid_partition_get_partno(p.partitionHandle)))
}
