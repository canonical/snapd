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
	"io"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/disks"
)

type tableSizeType int

const (
	Normal tableSizeType = 0
	Big                  = 1
	Small                = 2
)

type gptSuite struct {
	image     string
	size      uint64
	blockSize uint64
	tableSize tableSizeType
}

var (
	_ = Suite(&gptSuite{blockSize: 512, tableSize: Normal})
	_ = Suite(&gptSuite{blockSize: 512, tableSize: Small})
	_ = Suite(&gptSuite{blockSize: 512, tableSize: Big})
	_ = Suite(&gptSuite{blockSize: 4096, tableSize: Normal})
	_ = Suite(&gptSuite{blockSize: 4096, tableSize: Small})
	_ = Suite(&gptSuite{blockSize: 4096, tableSize: Big})
)

func (s *gptSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()
	suffix := ""
	if s.blockSize == 4096 {
		suffix = suffix + "_4k"
	}
	if s.tableSize == Small {
		suffix = suffix + "_small"
	}
	if s.tableSize == Big {
		suffix = suffix + "_big"
	}
	header := mylog.Check2(os.Open("testdata/gpt_header" + suffix))

	defer header.Close()
	footer := mylog.Check2(os.Open("testdata/gpt_footer" + suffix))

	defer footer.Close()
	s.image = filepath.Join(tmpdir, "image.img")
	image := mylog.Check2(os.OpenFile(s.image, os.O_WRONLY|os.O_CREATE, 0o666))

	defer image.Close()
	_ = mylog.Check2(io.Copy(image, header))

	// 128M - 1 block
	_ = mylog.Check2(image.Seek((128*1024*1024/int64(s.blockSize)-1)*int64(s.blockSize), os.SEEK_SET))

	io.Copy(image, footer)

	stat := mylog.Check2(os.Stat(s.image))

	size := stat.Size()
	c.Assert(size%int64(s.blockSize), Equals, int64(0))
	s.size = uint64(size) / s.blockSize
}

func (s *gptSuite) TestReadFirstLBA(c *C) {
	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	gptHeader := mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))


	c.Assert(uint64(gptHeader.CurrentLBA), Equals, uint64(1))
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, s.size-1)
}

func (s *gptSuite) TestReadLastLBA(c *C) {
	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(-int64(s.blockSize), 2))


	gptHeader := mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))


	c.Assert(uint64(gptHeader.CurrentLBA), Equals, s.size-1)
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, uint64(1))
}

func (s *gptSuite) messSignature(c *C) {
	f := mylog.Check2(os.OpenFile(s.image, os.O_RDWR, 0777))

	defer f.Close()
	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))

	_ = mylog.Check2(f.Write([]byte("NOTGPT")))

}

func (s *gptSuite) TestBadSignature(c *C) {
	s.messSignature(c)

	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	_ = mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))
	c.Assert(err, ErrorMatches, `GPT Header does not start with the magic string`)
}

func (s *gptSuite) messRevision(c *C) {
	f := mylog.Check2(os.OpenFile(s.image, os.O_RDWR, 0777))

	defer f.Close()
	_ = mylog.Check2(f.Seek(int64(s.blockSize)+8, 0))

	mylog.Check(binary.Write(f, binary.LittleEndian, uint32(0x12345678)))

}

func (s *gptSuite) TestBadRevision(c *C) {
	s.messRevision(c)

	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	_ = mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))
	c.Assert(err, ErrorMatches, `GPT header revision is not 1.0`)
}

func (s *gptSuite) messSize(c *C, newsize uint32) {
	f := mylog.Check2(os.OpenFile(s.image, os.O_RDWR, 0777))

	defer f.Close()
	_ = mylog.Check2(f.Seek(int64(s.blockSize)+8+4, 0))

	mylog.Check(binary.Write(f, binary.LittleEndian, newsize))

}

func (s *gptSuite) TestSmallSize(c *C) {
	s.messSize(c, 90)

	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	_ = mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))
	c.Assert(err, ErrorMatches, `GPT header size is smaller than the minimum valid size`)
}

func (s *gptSuite) TestBigSize(c *C) {
	s.messSize(c, uint32(s.blockSize)+3)

	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	_ = mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))
	c.Assert(err, ErrorMatches, `GPT header size is larger than the maximum supported size`)
}

func (s *gptSuite) messCRC(c *C) {
	f := mylog.Check2(os.OpenFile(s.image, os.O_RDWR, 0777))

	defer f.Close()
	_ = mylog.Check2(f.Seek(int64(s.blockSize)+8+4+4, 0))

	var crc uint32
	mylog.Check(binary.Read(f, binary.LittleEndian, &crc))

	_ = mylog.Check2(f.Seek(int64(s.blockSize)+8+4+4, 0))

	crc = crc + 1
	mylog.Check(binary.Write(f, binary.LittleEndian, crc))

}

func (s *gptSuite) TestBadCRC(c *C) {
	s.messCRC(c)

	f := mylog.Check2(os.Open(s.image))

	_ = mylog.Check2(f.Seek(int64(s.blockSize), 0))


	_ = mylog.Check2(disks.LoadGPTHeader(f, s.blockSize))
	c.Assert(err, ErrorMatches, `GPT header CRC32 checksum failed: [0-9]+ != [0-9]+`)
}

func (s *gptSuite) TestReadFile(c *C) {
	gptHeader := mylog.Check2(disks.ReadGPTHeader(s.image, s.blockSize))


	// Check that we got the first header
	c.Assert(uint64(gptHeader.CurrentLBA), Equals, uint64(1))
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, s.size-1)
}

func (s *gptSuite) TestReadFileFallback(c *C) {
	s.messSignature(c)
	gptHeader := mylog.Check2(disks.ReadGPTHeader(s.image, s.blockSize))


	// Check that we got the alternate header
	c.Assert(uint64(gptHeader.CurrentLBA), Equals, s.size-1)
	c.Assert(uint64(gptHeader.AlternateLBA), Equals, uint64(1))
}

func (s *gptSuite) messAlternateRevision(c *C) {
	f := mylog.Check2(os.OpenFile(s.image, os.O_RDWR, 0777))

	defer f.Close()
	_ = mylog.Check2(f.Seek(-int64(s.blockSize)+8, 2))

	mylog.Check(binary.Write(f, binary.LittleEndian, uint32(0x12345678)))

}

func (s *gptSuite) TestReadFileFail(c *C) {
	s.messSignature(c)
	s.messAlternateRevision(c)
	_ := mylog.Check2(disks.ReadGPTHeader(s.image, s.blockSize))

	// Check that we get the error from the main header
	c.Assert(err, ErrorMatches, `GPT Header does not start with the magic string`)
}

func (s *gptSuite) TestCalculateSize(c *C) {
	calculated := mylog.Check2(disks.CalculateLastUsableLBA(s.image, 128*1024*1024, s.blockSize))


	if s.tableSize == Small {
		size := 128 * 1024 * 1024 / int64(s.blockSize)
		alternateHeader := size - 1
		alternateTable := alternateHeader - 16*1024/int64(s.blockSize)
		lastUsable := alternateTable - 1
		c.Assert(calculated, Equals, uint64(lastUsable))
	} else {
		gptHeader := mylog.Check2(disks.ReadGPTHeader(s.image, s.blockSize))

		c.Assert(calculated, Equals, uint64(gptHeader.LastUsableLBA))
	}
}

func (s *gptSuite) TestCalculateSizeResized(c *C) {
	mylog.Check(exec.Command("truncate", "--size", "256M", s.image).Run())


	calculated := mylog.Check2(disks.CalculateLastUsableLBA(s.image, 256*1024*1024, s.blockSize))


	if s.tableSize == Small {
		size := 256 * 1024 * 1024 / int64(s.blockSize)
		alternateHeader := size - 1
		alternateTable := alternateHeader - 16*1024/int64(s.blockSize)
		lastUsable := alternateTable - 1
		c.Assert(calculated, Equals, uint64(lastUsable))
	} else {
		gptHeader := mylog.Check2(disks.ReadGPTHeader(s.image, s.blockSize))

		// We added 128*1024*2 sectors, we expect that exact value added
		c.Assert(calculated, Equals, uint64(gptHeader.LastUsableLBA)+128*1024*1024/s.blockSize)
	}
}
