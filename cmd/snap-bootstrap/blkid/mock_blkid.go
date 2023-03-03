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

import (
	"fmt"
)

type Constructor func(string) (AbstractBlkidProbe, error)

func MockBlkid(constr Constructor) func() {
	old := NewProbeFromFilename
	NewProbeFromFilename = constr
	return func() {
		NewProbeFromFilename = old
	}
}

func MockBlkidMap(probeMap map[string]*FakeBlkidProbe) func() {
	return MockBlkid(func(name string) (AbstractBlkidProbe, error) {
		value, ok := probeMap[name]
		if !ok {
			return nil, fmt.Errorf("not found")
		}
		return value, nil
	})
}

func BuildFakeProbe(values map[string]string) *FakeBlkidProbe {
	return &FakeBlkidProbe{values, &FakeBlkidPartlist{}}
}

func (p *FakeBlkidProbe) AddPartition(name, uuid string) {
	p.partlist.partitions = append(p.partlist.partitions, &FakeBlkidPartition{name, uuid})
}

type FakeBlkidPartition struct {
	name string
	uuid string
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

func (p *FakeBlkidProbe) EnableSuperblocks(value bool) {
}

func (p *FakeBlkidProbe) SetPartitionsFlags(flags int) {
}

func (p *FakeBlkidProbe) DoSafeprobe() error {
	return nil
}

func (p *FakeBlkidProbe) GetPartitions() (AbstractBlkidPartlist, error) {
	return p.partlist, nil
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
