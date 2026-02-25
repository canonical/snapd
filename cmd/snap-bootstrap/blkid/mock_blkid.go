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

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

type Constructor func(string) (AbstractBlkidProbe, error)

// MockBlkidProbeFromFilename mocks [NewProbeFromFilename]. Should only be used
// in tests.
//
// Keep the restore logic local instead of using testutil: this helper lives in
// production code so tests in other packages can call it, and importing
// testutil would leak a test-only dependency into production import graphs.
func MockBlkidProbeFromFilename(constr Constructor) func() {
	osutil.MustBeTestBinary("can only mock blkid probe in tests")

	backup := NewProbeFromFilename
	r := func() {
		NewProbeFromFilename = backup
	}
	NewProbeFromFilename = constr
	return r
}

func MockBlkidMap(probeMap map[string]*FakeBlkidProbe) func() {
	return MockBlkidProbeFromFilename(func(name string) (AbstractBlkidProbe, error) {
		value, ok := probeMap[name]
		if !ok {
			return nil, fmt.Errorf("not found")
		}
		return value, nil
	})
}

// MockBlkidProbeFromRange mocks [NewProbeFromRange]. Should only be used in
// tests.
//
// Keep the restore logic local instead of using testutil: this helper lives in
// production code so tests in other packages can call it, and importing
// testutil would leak a test-only dependency into production import graphs.
func MockBlkidProbeFromRange(f func(node string, start, size int64) (AbstractBlkidProbe, error)) func() {
	osutil.MustBeTestBinary("can only mock blkid probe in tests")

	backup := NewProbeFromRange
	r := func() {
		NewProbeFromRange = backup
	}
	NewProbeFromRange = f
	return r
}

func MockBlkidPartitionMap(probeMap map[int64]*FakeBlkidProbe) func() {
	return MockBlkidProbeFromRange(func(node string, start, size int64) (AbstractBlkidProbe, error) {
		value, ok := probeMap[start]
		if !ok {
			return nil, fmt.Errorf("not found")
		}
		return value, nil
	})
}

func BuildFakeProbe(values map[string]string) *FakeBlkidProbe {
	return &FakeBlkidProbe{values, &FakeBlkidPartlist{}}
}

func (p *FakeBlkidProbe) AddEmptyPartitionProbe(start int64) *FakeBlkidProbe {
	p.partlist.partitions = append(p.partlist.partitions, &FakeBlkidPartition{0, "", "", start})
	return BuildFakeProbe(make(map[string]string))
}

func (p *FakeBlkidProbe) AddPartitionProbe(number int, name, uuid string, start int64) *FakeBlkidProbe {
	p.partlist.partitions = append(p.partlist.partitions, &FakeBlkidPartition{number, name, uuid, start})

	partition_values := make(map[string]string)
	if name == "ubuntu-seed" {
		partition_values["TYPE"] = "vfat"
	} else {
		partition_values["TYPE"] = "ext4"
	}
	partition_values["LABEL"] = name
	partition_values["UUID"] = uuid
	return BuildFakeProbe(partition_values)
}

type FakeBlkidPartition struct {
	number int
	name   string
	uuid   string
	start  int64
}

type FakeBlkidPartlist struct {
	partitions []*FakeBlkidPartition
}

type FakeBlkidProbe struct {
	values   map[string]string
	partlist *FakeBlkidPartlist
}

func (p *FakeBlkidProbe) LookupValue(entryName string) (string, error) {
	value, ok := p.values[entryName]
	if !ok {
		return "", fmt.Errorf("Probe value was not found: %s", entryName)
	}
	return value, nil
}

func (p *FakeBlkidProbe) Close() {
}

func (p *FakeBlkidProbe) EnablePartitions(value bool) {
}

func (p *FakeBlkidProbe) SetPartitionsFlags(flags int) {
}

func (p *FakeBlkidProbe) EnableSuperblocks(value bool) {
}

func (p *FakeBlkidProbe) SetSuperblockFlags(flags int) {
}

func (p *FakeBlkidProbe) DoSafeprobe() error {
	return nil
}

func (p *FakeBlkidProbe) GetPartitions() (AbstractBlkidPartlist, error) {
	return p.partlist, nil
}

func (p *FakeBlkidProbe) GetSectorSize() (uint, error) {
	return 512, nil
}

func (p *FakeBlkidPartlist) GetPartitions() []AbstractBlkidPartition {
	ret := make([]AbstractBlkidPartition, len(p.partitions))
	for i, partition := range p.partitions {
		ret[i] = partition
	}
	return ret
}

func (p *FakeBlkidPartition) GetName() string {
	return p.name
}

func (p *FakeBlkidPartition) GetUUID() string {
	return p.uuid
}

func (p *FakeBlkidPartition) GetStart() int64 {
	return p.start
}

func (p *FakeBlkidPartition) GetSize() int64 {
	return 0
}

func (p *FakeBlkidPartition) GetNumber() int {
	return p.number
}
