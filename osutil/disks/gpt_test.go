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

package disks_test

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/disks"
)

type gptSuite struct {
	image string
	size  uint64
}

var _ = Suite(&gptSuite{})

func (s *gptSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()
	header, err := os.Open("testdata/gpt_header")
	c.Assert(err, IsNil)
	defer header.Close()
	footer, err := os.Open("testdata/gpt_footer")
	c.Assert(err, IsNil)
	defer footer.Close()
	s.image = filepath.Join(tmpdir, "image.img")
	image, err := os.OpenFile(s.image, os.O_WRONLY|os.O_CREATE, 0o666)
	c.Assert(err, IsNil)
	defer image.Close()
	_, err = io.Copy(image, header)
	c.Assert(err, IsNil)
	// 128M - 1 block
	_, err = image.Seek((128*1024*2-1)*512, os.SEEK_SET)
	c.Assert(err, IsNil)
	io.Copy(image, footer)

	stat, err := os.Stat(s.image)
	c.Assert(err, IsNil)
	size := stat.Size()
	c.Assert(size%512, Equals, int64(0))
	s.size = uint64(size) / 512
}

func (s *gptSuite) TestReadFirstLBA(c *C) {
	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	gptHeader, err := disks.LoadGPTHeader(f)
	c.Assert(err, IsNil)

	c.Assert(uint64(gptHeader.CurrentLBA), Equals, uint64(1))
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, s.size-1)
}

func (s *gptSuite) TestReadLastLBA(c *C) {
	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(-512, 2)
	c.Assert(err, IsNil)

	gptHeader, err := disks.LoadGPTHeader(f)
	c.Assert(err, IsNil)

	c.Assert(uint64(gptHeader.CurrentLBA), Equals, s.size-1)
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, uint64(1))
}

func (s *gptSuite) messSignature(c *C) {
	f, err := os.OpenFile(s.image, os.O_RDWR, 0777)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)
	_, err = f.Write([]byte("NOTGPT"))
	c.Assert(err, IsNil)
}

func (s *gptSuite) TestBadSignature(c *C) {
	s.messSignature(c)

	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	_, err = disks.LoadGPTHeader(f)
	c.Assert(err, ErrorMatches, `GPT Header does not start with the magic string`)
}

func (s *gptSuite) messRevision(c *C) {
	f, err := os.OpenFile(s.image, os.O_RDWR, 0777)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Seek(512+8, 0)
	c.Assert(err, IsNil)
	err = binary.Write(f, binary.LittleEndian, uint32(0x12345678))
	c.Assert(err, IsNil)
}

func (s *gptSuite) TestBadRevision(c *C) {
	s.messRevision(c)

	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	_, err = disks.LoadGPTHeader(f)
	c.Assert(err, ErrorMatches, `GPT header revision is not 1.0`)
}

func (s *gptSuite) messSize(c *C, newsize uint32) {
	f, err := os.OpenFile(s.image, os.O_RDWR, 0777)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Seek(512+8+4, 0)
	c.Assert(err, IsNil)
	err = binary.Write(f, binary.LittleEndian, newsize)
	c.Assert(err, IsNil)
}

func (s *gptSuite) TestSmallSize(c *C) {
	s.messSize(c, 90)

	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	_, err = disks.LoadGPTHeader(f)
	c.Assert(err, ErrorMatches, `GPT header size is smaller than the minimum valid size`)
}

func (s *gptSuite) TestBigSize(c *C) {
	s.messSize(c, 514)

	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	_, err = disks.LoadGPTHeader(f)
	c.Assert(err, ErrorMatches, `GPT header size is larger than the maximum supported size`)
}

func (s *gptSuite) messCRC(c *C) {
	f, err := os.OpenFile(s.image, os.O_RDWR, 0777)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Seek(512+8+4+4, 0)
	c.Assert(err, IsNil)
	var crc uint32
	err = binary.Read(f, binary.LittleEndian, &crc)
	c.Assert(err, IsNil)
	_, err = f.Seek(512+8+4+4, 0)
	c.Assert(err, IsNil)
	crc = crc + 1
	err = binary.Write(f, binary.LittleEndian, crc)
	c.Assert(err, IsNil)
}

func (s *gptSuite) TestBadCRC(c *C) {
	s.messCRC(c)

	f, err := os.Open(s.image)
	c.Assert(err, IsNil)
	_, err = f.Seek(512, 0)
	c.Assert(err, IsNil)

	_, err = disks.LoadGPTHeader(f)
	c.Assert(err, ErrorMatches, `GPT header CRC32 checksum failed: [0-9]+ != [0-9]+`)
}

func (s *gptSuite) TestReadFile(c *C) {
	gptHeader, err := disks.ReadGPTHeader(s.image)
	c.Assert(err, IsNil)

	// Check that we got the first header
	c.Assert(uint64(gptHeader.CurrentLBA), Equals, uint64(1))
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, s.size-1)
}

func (s *gptSuite) TestReadFileFallback(c *C) {
	s.messSignature(c)
	gptHeader, err := disks.ReadGPTHeader(s.image)
	c.Assert(err, IsNil)

	// Check that we got the alternate header
	c.Assert(uint64(gptHeader.CurrentLBA), Equals, s.size-1)
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, uint64(1))
}

func (s *gptSuite) messAlternateRevision(c *C) {
	f, err := os.OpenFile(s.image, os.O_RDWR, 0777)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.Seek(-512+8, 2)
	c.Assert(err, IsNil)
	err = binary.Write(f, binary.LittleEndian, uint32(0x12345678))
	c.Assert(err, IsNil)
}

func (s *gptSuite) TestReadFileFail(c *C) {
	s.messSignature(c)
	s.messAlternateRevision(c)
	_, err := disks.ReadGPTHeader(s.image)

	// Check that we get the error from the main header
	c.Assert(err, ErrorMatches, `GPT Header does not start with the magic string`)
}

func (s *gptSuite) TestCalculateSize(c *C) {
	if _, err := exec.LookPath("blockdev"); err != nil && errors.Is(err, exec.ErrNotFound) {
		c.Skip("blockdev command not available")
	}
	calculated, err := disks.CalculateLastUsableLBA(s.image)
	c.Assert(err, IsNil)
	gptHeader, err := disks.ReadGPTHeader(s.image)
	c.Assert(err, IsNil)

	c.Assert(uint64(gptHeader.LastUsableLBA), Equals, calculated)
}

func (s *gptSuite) TestCalculateSizeResized(c *C) {
	if _, err := exec.LookPath("blockdev"); err != nil && errors.Is(err, exec.ErrNotFound) {
		c.Skip("blockdev command not available")
	}
	err := exec.Command("truncate", "--size", "256M", s.image).Run()
	c.Assert(err, IsNil)

	calculated, err := disks.CalculateLastUsableLBA(s.image)
	c.Assert(err, IsNil)
	gptHeader, err := disks.ReadGPTHeader(s.image)
	c.Assert(err, IsNil)
	// We added 128*1024*2 sectors, we expect that exact value added
	c.Assert(uint64(gptHeader.LastUsableLBA)+128*1024*2, Equals, calculated)
}
