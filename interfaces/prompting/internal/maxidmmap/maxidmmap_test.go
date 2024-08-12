// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package maxidmmap_test

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/osutil"
)

func Test(t *testing.T) { TestingT(t) }

type maxidmmapSuite struct {
	tmpdir    string
	maxIDPath string
}

var _ = Suite(&maxidmmapSuite{})

func (s *maxidmmapSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	s.maxIDPath = filepath.Join(s.tmpdir, "max-id")
}

func (s *maxidmmapSuite) TestMaxIDMmapOpenNextIDCloseInvalid(c *C) {
	// First try with no existin max ID file
	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(s.maxIDPath)
	c.Check(err, IsNil)
	c.Check(maxIDMmap, NotNil)
	s.checkWrittenMaxID(c, 0)
	id, err := maxIDMmap.NextID()
	c.Check(err, IsNil)
	c.Check(id, Equals, prompting.IDType(1))
	s.checkWrittenMaxID(c, 1)

	maxIDMmap.Close()

	// Now try with various invalid max ID files
	for _, initial := range [][]byte{
		[]byte(""),
		[]byte("foo"),
		[]byte("1234"),
		[]byte("1234567"),
		[]byte("123456789"),
	} {
		osutil.AtomicWriteFile(s.maxIDPath, initial, 0600, 0)
		maxIDMmap, err = maxidmmap.OpenMaxIDMmap(s.maxIDPath)
		c.Check(err, IsNil)
		c.Check(maxIDMmap, NotNil)
		s.checkWrittenMaxID(c, 0)
		id, err := maxIDMmap.NextID()
		c.Check(err, IsNil)
		c.Check(id, Equals, prompting.IDType(1))
		s.checkWrittenMaxID(c, 1)
		maxIDMmap.Close()
	}
}

func (s *maxidmmapSuite) TestMaxIDMmapOpenNextIDCloseValid(c *C) {
	for _, testCase := range []struct {
		initial uint64
		nextID  prompting.IDType
	}{
		{
			0,
			1,
		},
		{
			1,
			2,
		},
		{
			0x1000000000000001,
			0x1000000000000002,
		},
		{
			0x0123456789ABCDEF,
			0x0123456789ABCDF0,
		},
		{
			0xDEADBEEFDEADBEEF,
			0xDEADBEEFDEADBEF0,
		},
	} {
		var initialData [8]byte
		*(*uint64)(unsafe.Pointer(&initialData[0])) = testCase.initial
		osutil.AtomicWriteFile(s.maxIDPath, initialData[:], 0600, 0)
		func() {
			maxIDMmap, err := maxidmmap.OpenMaxIDMmap(s.maxIDPath)
			c.Check(err, IsNil)
			c.Check(maxIDMmap, NotNil)
			defer maxIDMmap.Close()

			s.checkWrittenMaxID(c, testCase.initial)
			id, err := maxIDMmap.NextID()
			c.Check(err, IsNil)
			c.Check(id, Equals, testCase.nextID)
			s.checkWrittenMaxID(c, uint64(testCase.nextID))
		}()
	}
}

func (s *maxidmmapSuite) checkWrittenMaxID(c *C, id uint64) {
	data, err := os.ReadFile(s.maxIDPath)
	c.Assert(err, IsNil)
	c.Assert(data, HasLen, 8)
	writtenID := *(*uint64)(unsafe.Pointer(&data[0]))
	c.Assert(writtenID, Equals, id)
}

func (s *maxidmmapSuite) TestMaxIDMmapClose(c *C) {
	maxIDMmap, err := maxidmmap.OpenMaxIDMmap(s.maxIDPath)
	c.Assert(err, IsNil)
	c.Assert(maxIDMmap, NotNil)
	c.Check(maxIDMmap.IsClosed(), Equals, false)
	maxIDMmap.Close()
	c.Check(maxIDMmap.IsClosed(), Equals, true)
	_, err = maxIDMmap.NextID()
	c.Check(err, Equals, maxidmmap.ErrMaxIDMmapClosed)
	maxIDMmap.Close() // Close should be idempotent
}
