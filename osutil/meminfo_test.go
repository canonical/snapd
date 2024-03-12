// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package osutil_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type meminfoSuite struct{}

var _ = Suite(&meminfoSuite{})

const meminfoExampleFromLiveSystem = `MemTotal:       32876680 kB
MemFree:         3478104 kB
MemAvailable:   20527364 kB
Buffers:         1584432 kB
Cached:         14550292 kB
SwapCached:            0 kB
Active:          8344864 kB
Inactive:       16209412 kB
Active(anon):     139828 kB
Inactive(anon):  8944656 kB
Active(file):    8205036 kB
Inactive(file):  7264756 kB
Unevictable:        2152 kB
Mlocked:            2152 kB
SwapTotal:             0 kB
SwapFree:              0 kB
Dirty:               804 kB
Writeback:             4 kB
AnonPages:       8201352 kB
Mapped:          1223272 kB
Shmem:            697812 kB
KReclaimable:    2043404 kB
Slab:            2423344 kB
SReclaimable:    2043404 kB
SUnreclaim:       379940 kB
KernelStack:       37392 kB
PageTables:        97644 kB
NFS_Unstable:          0 kB
Bounce:                0 kB
WritebackTmp:          0 kB
CommitLimit:    16438340 kB
Committed_AS:   30191756 kB
VmallocTotal:   34359738367 kB
VmallocUsed:      160500 kB
VmallocChunk:          0 kB
Percpu:            24256 kB
HardwareCorrupted:     0 kB
AnonHugePages:    120832 kB
ShmemHugePages:        0 kB
ShmemPmdMapped:        0 kB
FileHugePages:         0 kB
FilePmdMapped:         0 kB
CmaTotal:              0 kB
CmaFree:               0 kB
HugePages_Total:       0
HugePages_Free:        0
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:               0 kB
DirectMap4k:     3059376 kB
DirectMap2M:    23089152 kB
DirectMap1G:     8388608 kB
`

const meminfoExampleFromPi3 = `MemTotal:         929956 kB
MemFree:           83420 kB
MemAvailable:     676936 kB
Buffers:           84036 kB
Cached:           439980 kB
SwapCached:            0 kB
Active:           371964 kB
Inactive:         280064 kB
Active(anon):      86560 kB
Inactive(anon):     9152 kB
Active(file):     285404 kB
Inactive(file):   270912 kB
Unevictable:           0 kB
Mlocked:               0 kB
SwapTotal:             0 kB
SwapFree:              0 kB
Dirty:                 8 kB
Writeback:             0 kB
AnonPages:        128052 kB
Mapped:            53224 kB
Shmem:             13360 kB
KReclaimable:      52404 kB
Slab:             118608 kB
SReclaimable:      52404 kB
SUnreclaim:        66204 kB
KernelStack:        2928 kB
PageTables:         1552 kB
NFS_Unstable:          0 kB
Bounce:                0 kB
WritebackTmp:          0 kB
CommitLimit:      464976 kB
Committed_AS:     496260 kB
VmallocTotal:   135290159040 kB
VmallocUsed:       13700 kB
VmallocChunk:          0 kB
Percpu:             2768 kB
CmaTotal:         131072 kB
CmaFree:            8664 kB
`

func (s *meminfoSuite) TestMemInfoHappy(c *C) {
	p := filepath.Join(c.MkDir(), "meminfo")
	restore := osutil.MockProcMeminfo(p)
	defer restore()

	c.Assert(os.WriteFile(p, []byte(meminfoExampleFromLiveSystem), 0644), IsNil)

	mem, err := osutil.TotalUsableMemory()
	c.Assert(err, IsNil)
	c.Check(mem, Equals, uint64(32876680)*1024)

	c.Assert(os.WriteFile(p, []byte(`MemTotal:    1234 kB`), 0644), IsNil)

	mem, err = osutil.TotalUsableMemory()
	c.Assert(err, IsNil)
	c.Check(mem, Equals, uint64(1234)*1024)

	const meminfoReorderedWithEmptyLine = `MemAvailable:   20527370 kB

MemTotal:       32876699 kB
MemFree:         3478104 kB
`
	c.Assert(os.WriteFile(p, []byte(meminfoReorderedWithEmptyLine), 0644), IsNil)

	mem, err = osutil.TotalUsableMemory()
	c.Assert(err, IsNil)
	c.Check(mem, Equals, uint64(32876699)*1024)

	c.Assert(os.WriteFile(p, []byte(meminfoExampleFromPi3), 0644), IsNil)

	// CmaTotal is taken correctly into account
	mem, err = osutil.TotalUsableMemory()
	c.Assert(err, IsNil)
	c.Check(mem, Equals, uint64(929956-131072)*1024)
}

func (s *meminfoSuite) TestMemInfoFromHost(c *C) {
	mem, err := osutil.TotalUsableMemory()
	c.Assert(err, IsNil)
	c.Check(mem > uint64(32*1024*1024),
		Equals, true, Commentf("unexpected system memory %v", mem))
}

func (s *meminfoSuite) TestMemInfoUnhappy(c *C) {
	p := filepath.Join(c.MkDir(), "meminfo")
	restore := osutil.MockProcMeminfo(p)
	defer restore()

	const noTotalMem = `MemFree:         3478104 kB
MemAvailable:   20527364 kB
Buffers:         1584432 kB
Cached:         14550292 kB
`
	const notkBTotalMem = `MemTotal:         3478104 MB
`
	const missingFieldsTotalMem = `MemTotal:  1234
`
	const badTotalMem = `MemTotal:  abcdef kB
`
	const hexTotalMem = `MemTotal:  0xabcdef kB
`

	for _, tc := range []struct {
		content, err string
	}{
		{
			content: noTotalMem,
			err:     `cannot determine the total amount of memory in the system from .*/meminfo`,
		}, {
			content: notkBTotalMem,
			err:     `cannot process unexpected meminfo entry "MemTotal:         3478104 MB"`,
		}, {
			content: missingFieldsTotalMem,
			err:     `cannot process unexpected meminfo entry "MemTotal:  1234"`,
		}, {
			content: badTotalMem,
			err:     `cannot convert memory size value: strconv.ParseUint: parsing "abcdef": invalid syntax`,
		}, {
			content: hexTotalMem,
			err:     `cannot convert memory size value: strconv.ParseUint: parsing "0xabcdef": invalid syntax`,
		},
	} {
		c.Assert(os.WriteFile(p, []byte(tc.content), 0644), IsNil)
		mem, err := osutil.TotalUsableMemory()
		c.Assert(err, ErrorMatches, tc.err)
		c.Check(mem, Equals, uint64(0))
	}
}
