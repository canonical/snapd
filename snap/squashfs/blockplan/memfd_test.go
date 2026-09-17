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

package blockplan_test

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type memFileSuite struct{}

var _ = Suite(&memFileSuite{})

// Both backings of a scratch file have to offer the same three things: a path a
// child process can open, a Reset that truly empties the file, and a Close that
// leaves nothing behind.
//
// The memfd backing is what every apply on a current kernel uses. The disk one
// exists for a kernel without memfd_create, where it is the difference between a
// working apply and none -- and being unreachable on every machine this is
// tested on, it is tested here deliberately.
func (s *memFileSuite) TestScratchFileBackings(c *C) {
	backings := []struct {
		name   string
		open   func(string) (*blockplan.MemFile, error)
		onDisk bool
	}{
		{"memfd", blockplan.NewMemFile, false},
		{"disk fallback", blockplan.NewDiskFile, true},
	}

	for _, b := range backings {
		comment := Commentf("%s backing", b.name)
		m, err := b.open("scratch")
		c.Assert(err, IsNil, comment)

		c.Check(m.OnDisk(), Equals, b.onDisk, comment)

		// The path is how xz and hpatchz are handed the file, so it has to
		// resolve by name and not merely by descriptor.
		_, err = os.Stat(m.Path())
		c.Assert(err, IsNil, comment)

		payload := []byte("scratch contents that a patch run would hold")
		_, err = m.File().Write(payload)
		c.Assert(err, IsNil, comment)
		// Reading back through the path is the child's view, which is the
		// one that matters: a file that only a descriptor reaches would
		// pass a Read here and still fail an xz invocation.
		got, err := os.ReadFile(m.Path())
		c.Assert(err, IsNil, comment)
		c.Check(got, DeepEquals, payload, comment)

		// Reset has to leave an empty file with the cursor at 0. Reuse
		// across patch runs is the whole point of the type, and a stale
		// tail would silently corrupt the next run's window.
		c.Assert(m.Reset(), IsNil, comment)
		fi, err := os.Stat(m.Path())
		c.Assert(err, IsNil, comment)
		c.Check(fi.Size(), Equals, int64(0), comment)

		shorter := []byte("shorter")
		_, err = m.File().Write(shorter)
		c.Assert(err, IsNil, comment)
		got, err = os.ReadFile(m.Path())
		c.Assert(err, IsNil, comment)
		c.Check(got, DeepEquals, shorter, comment)

		// Close must leave nothing behind on a device that may have very
		// little room. The memfd case has nothing to unlink; the disk case
		// does.
		path := m.Path()
		c.Assert(m.Close(), IsNil, comment)
		if b.onDisk {
			c.Check(osutil.FileExists(path), Equals, false, comment)
		}
	}
}

func (s *memFileSuite) TestNewMemFileWith(c *C) {
	data := []byte("a source window, or a patch, that the caller already has")
	m, err := blockplan.NewMemFileWith("window", data)
	c.Assert(err, IsNil)
	defer m.Close()

	// The point of the helper is a file a child can open at once, so the
	// contents have to be there under the path rather than buffered here.
	got, err := os.ReadFile(m.Path())
	c.Assert(err, IsNil)
	c.Check(got, DeepEquals, data)
}
