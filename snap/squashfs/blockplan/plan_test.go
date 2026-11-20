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
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type planSuite struct{}

var _ = Suite(&planSuite{})

// testPlanHeader is a header the container accepts, for a test to change one
// field of and see which rule fires. The geometry is nominal -- an 8 KiB target
// with its metadata a page in -- since nothing here reads the image itself.
func testPlanHeader() blockplan.PlanHeader {
	return blockplan.PlanHeader{
		FormatVersion:         blockplan.PlanFormatVersion,
		ToolsVersion:          blockplan.PlanToolsVersion,
		PatchTool:             blockplan.PatchToolHdiffz,
		BlockSize:             testBlockSize,
		MaxRunUSize:           8 << 20,
		TargetSize:            8192,
		TargetBytesUsed:       8000,
		TargetInodeTableStart: 4096,
	}
}

// newTestPlan returns a writer holding the smallest set of sections openPlan
// accepts: SEC_SB, SEC_MDFRAME and SEC_INSTR present, SEC_PAY last. Everything
// is stored uncompressed, so the tests that only need a well-formed container
// run on a machine with no compressor at all.
func newTestPlan(instr, pay []byte) *blockplan.PlanWriter {
	var w blockplan.PlanWriter
	w.Header = testPlanHeader()
	w.AddSection(blockplan.SecSB, bytes.Repeat([]byte{0x5a}, 96))
	w.AddSection(blockplan.SecMDFrame, []byte{0x00, 0x80, 0x01})
	w.AddSection(blockplan.SecMDTail, []byte("tail"))
	w.AddSection(blockplan.SecInstr, instr)
	w.AddSection(blockplan.SecPay, pay)
	return &w
}

// TestContainerRoundTrip walks a delta out and back in through the writer and
// the reader: the header, the sections the applier reads eagerly, and the one it
// leaves streamed.
func (s *planSuite) TestContainerRoundTrip(c *C) {
	requireTools(c, "xz")
	ctx := context.Background()
	comp, err := blockplan.NewCompressor(blockplan.CompressorXz, 1)
	c.Assert(err, IsNil)

	instr := bytes.Repeat([]byte("instruction stream, highly compressible "), 500)
	pay := bytes.Repeat([]byte{0x11, 0x22}, 4096)
	sb := bytes.Repeat([]byte{0x5a}, 96)

	var w blockplan.PlanWriter
	w.Header = testPlanHeader()
	w.Header.InstrCount = 7
	w.Header.SourceSize = 1234
	w.Header.SourceSHA256[0] = 0xaa
	w.Header.TargetSHA256[31] = 0xbb

	w.AddSection(blockplan.SecSB, sb)
	w.AddSection(blockplan.SecMDFrame, []byte{0x00, 0x80, 0x01})
	w.AddSection(blockplan.SecMDTail, []byte("tail"))
	c.Assert(w.AddSectionCompressed(ctx, blockplan.SecInstr, instr, comp), IsNil)
	w.AddSection(blockplan.SecPay, pay)

	var buf bytes.Buffer
	c.Assert(w.Emit(&buf), IsNil)

	pr, err := blockplan.OpenPlan(bytes.NewReader(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Check(pr.Header.BlockSize, Equals, uint32(testBlockSize))
	c.Check(pr.Header.InstrCount, Equals, uint32(7))
	c.Check(pr.Header.SourceSize, Equals, uint64(1234))
	c.Check(pr.Header.PatchTool, Equals, blockplan.PatchToolHdiffz)
	c.Check(pr.Header.SourceSHA256[0], Equals, byte(0xaa))
	c.Check(pr.Header.TargetSHA256[31], Equals, byte(0xbb))

	c.Check(bytes.Equal(pr.Section(blockplan.SecSB), sb), Equals, true,
		Commentf("SEC_SB did not round trip"))
	c.Check(bytes.Equal(pr.Section(blockplan.SecInstr), instr), Equals, true,
		Commentf("SEC_INSTR did not round trip through the section codec"))
	// An absent section is how "nothing changed here" is expressed, so it has
	// to read as absent rather than as empty.
	c.Check(pr.HasSection(blockplan.SecMDPatch), Equals, false,
		Commentf("absent SEC_MDPATCH reported as present"))

	// SEC_PAY stays streamed: it is the one section that scales with how much
	// of the image changed, and materializing it would undo the memory bound
	// the whole format is built around.
	c.Check(pr.HasSection(blockplan.SecPay), Equals, false,
		Commentf("SEC_PAY was materialized eagerly"))
	c.Check(pr.PayLen(), Equals, int64(len(pay)))
	got, err := io.ReadAll(pr.Pay())
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(got, pay), Equals, true,
		Commentf("SEC_PAY did not round trip, first difference at %d", firstDiff(got, pay)))

	// And compressing the instruction stream has to be pulling its weight, or
	// the section codec is costing a tool dependency for nothing.
	for _, e := range pr.Entries() {
		if e.ID != blockplan.SecInstr {
			continue
		}
		c.Check(e.Codec, Equals, comp.SectionCodec(),
			Commentf("SEC_INSTR was not stored under the compressor's own section codec"))
		c.Check(e.StoredLen < e.RawLen/10, Equals, true,
			Commentf("SEC_INSTR compressed %d -> %d, expected far better", e.RawLen, e.StoredLen))
	}
}

// TestDetectsCorruptionAndTruncation is the applier's front door: a delta
// arrives over the network, so every byte of it from the section table onward
// has to be either verified or refused before it is used to assemble an image.
//
// Header corruption deliberately is not a parse-time failure: most of the header
// is digests and geometry that only mean anything once the target exists, and
// those are checked while applying.
func (s *planSuite) TestDetectsCorruptionAndTruncation(c *C) {
	pay := bytes.Repeat([]byte{0x11, 0x22}, 4096)
	w := newTestPlan(bytes.Repeat([]byte("instructions "), 100), pay)

	var buf bytes.Buffer
	c.Assert(w.Emit(&buf), IsNil)
	whole := buf.Bytes()

	pr, err := blockplan.OpenPlan(bytes.NewReader(whole))
	c.Assert(err, IsNil)

	// Every single-byte corruption from the section table onward must be
	// caught, by the table's own consistency rules or by a section CRC. Walk a
	// sample rather than all of them, since the payload is large.
	start := blockplan.PlanHeaderSize + len(pr.Entries())*blockplan.SectionEntrySize
	eagerEnd := len(whole) - len(pay)
	for off := start; off < eagerEnd; off += 37 {
		bad := append([]byte(nil), whole...)
		bad[off] ^= 0x01
		_, err := blockplan.OpenPlan(bytes.NewReader(bad))
		c.Check(err, NotNil, Commentf("flipping byte %d of the delta was not detected", off))
	}

	// Truncation must be caught too, at any point.
	for _, n := range []int{0, 1, 64, blockplan.PlanHeaderSize, blockplan.PlanHeaderSize + 8, eagerEnd - 1} {
		_, err := blockplan.OpenPlan(bytes.NewReader(whole[:n]))
		c.Check(err, NotNil, Commentf("delta truncated to %d bytes was accepted", n))
	}
}

// TestFileBackedPayloadIsWrittenWhole covers the writer's other backing. SEC_PAY
// is accumulated in a scratch file while the plan is being built, so emitting it
// means rewinding a file whose offset is at its end -- and a delta whose payload
// began a few bytes in would assemble a wrong image rather than fail.
func (s *planSuite) TestFileBackedPayloadIsWrittenWhole(c *C) {
	pay := bytes.Repeat([]byte{0x33, 0x44, 0x55}, 5000)
	scratch, err := blockplan.NewMemFileWith("pay", pay)
	c.Assert(err, IsNil)
	defer scratch.Close()

	w := &blockplan.PlanWriter{Header: testPlanHeader()}
	w.AddSection(blockplan.SecSB, bytes.Repeat([]byte{0x5a}, 96))
	w.AddSection(blockplan.SecMDFrame, []byte{0x00, 0x80, 0x01})
	w.AddSection(blockplan.SecInstr, []byte("instructions"))
	w.AddSectionFile(blockplan.SecPay, scratch.File(), len(pay), crc32.ChecksumIEEE(pay))

	var buf bytes.Buffer
	c.Assert(w.Emit(&buf), IsNil)

	pr, err := blockplan.OpenPlan(bytes.NewReader(buf.Bytes()))
	c.Assert(err, IsNil)
	c.Check(pr.PayLen(), Equals, int64(len(pay)))
	got, err := io.ReadAll(pr.Pay())
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(got, pay), Equals, true,
		Commentf("a file-backed SEC_PAY did not round trip, first difference at %d", firstDiff(got, pay)))
}

// TestHeaderRefusals covers the header's own rules. They are the cheapest
// refusals in the format -- 128 bytes, before a single section is read -- and
// each one guards a field the applier would otherwise size a buffer or place a
// region from.
func (s *planSuite) TestHeaderRefusals(c *C) {
	tests := []struct {
		name   string
		mutate func(h *blockplan.PlanHeader)
		err    string
	}{{
		name:   "a format version this build cannot read",
		mutate: func(h *blockplan.PlanHeader) { h.FormatVersion = 9 },
		err:    `unsupported block-plan format version 9`,
	}, {
		name:   "a different generation of the delta tooling",
		mutate: func(h *blockplan.PlanHeader) { h.ToolsVersion = 9 },
		err:    `delta was made with tools version 9, this build is 1`,
	}, {
		name:   "flags this build does not know",
		mutate: func(h *blockplan.PlanHeader) { h.Flags = 1 },
		err:    `delta header sets unknown flags 0x0001`,
	}, {
		name:   "no sections at all",
		mutate: func(h *blockplan.PlanHeader) { h.SectionCount = 0 },
		err:    `delta declares 0 sections`,
	}, {
		name:   "a block size no LZMA2 dictionary can express",
		mutate: func(h *blockplan.PlanHeader) { h.BlockSize = 100000 },
		err:    `delta block size 100000: .*`,
	}, {
		// The run cap bounds the applier's peak memory, and a cap below one
		// block would make even a single block's run unencodable.
		name:   "a run cap below one block",
		mutate: func(h *blockplan.PlanHeader) { h.MaxRunUSize = 4096 },
		err:    `delta run cap 4096 is below one block \(131072\)`,
	}, {
		name:   "an inode table past bytes used",
		mutate: func(h *blockplan.PlanHeader) { h.TargetInodeTableStart = 9000 },
		err:    `delta target geometry is not ordered: inode_table=9000 bytes_used=8000 size=8192`,
	}, {
		name:   "bytes used past the image size",
		mutate: func(h *blockplan.PlanHeader) { h.TargetBytesUsed = 9999 },
		err:    `delta target geometry is not ordered: inode_table=4096 bytes_used=9999 size=8192`,
	}}

	for _, tc := range tests {
		comment := Commentf("%s", tc.name)
		h := testPlanHeader()
		h.SectionCount = 1
		tc.mutate(&h)
		_, err := blockplan.ParsePlanHeader(h.Marshal())
		c.Check(err, ErrorMatches, tc.err, comment)
	}

	// The unmutated header, or the table above proves nothing.
	h := testPlanHeader()
	h.SectionCount = 1
	_, err := blockplan.ParsePlanHeader(h.Marshal())
	c.Check(err, IsNil)

	_, err = blockplan.ParsePlanHeader(make([]byte, 32))
	c.Check(err, ErrorMatches, `short delta header: 32 bytes`)

	// A pseudo-file delta -- "sqpf", the snap-1-1-xdelta3 magic -- must not be
	// mistaken for a block plan: the two formats share a dispatch on magic, and
	// only the magic tells them apart.
	old := make([]byte, blockplan.PlanHeaderSize)
	binary.LittleEndian.PutUint32(old, 0x66707173)
	_, err = blockplan.ParsePlanHeader(old)
	c.Check(err, ErrorMatches, `not a block-plan delta \(magic 0x66707173\)`)
}

// rawSection is one row of a hand-built section table: the entry as it should be
// written, with the lengths and the CRC filled in from data unless the row set
// them itself.
type rawSection struct {
	entry blockplan.SectionEntry
	data  []byte
}

// rawPlan assembles a delta from an explicit header and section table, so a test
// can present a table no writer would ever produce.
func rawPlan(h blockplan.PlanHeader, sections []rawSection) []byte {
	h.SectionCount = uint16(len(sections))
	out := h.Marshal()
	entries := make([]blockplan.SectionEntry, len(sections))
	for i, s := range sections {
		e := s.entry
		if e.StoredLen == 0 {
			e.StoredLen = uint32(len(s.data))
		}
		if e.RawLen == 0 {
			e.RawLen = e.StoredLen
		}
		e.CRC = crc32.ChecksumIEEE(s.data)
		entries[i] = e
	}
	for _, e := range entries {
		out = append(out, e.Marshal()...)
	}
	for _, s := range sections {
		out = append(out, s.data...)
	}
	return out
}

// TestSectionTableRefusals covers the rules openPlan enforces over the table
// itself. Each of them holds an assumption the applier makes later: that a
// section id means one thing, that the sections it needs are there, that the
// streamed one is last and stored as it arrived, and that no section can be made
// to allocate an unbounded amount of memory.
func (s *planSuite) TestSectionTableRefusals(c *C) {
	sb := rawSection{entry: blockplan.SectionEntry{ID: blockplan.SecSB}, data: bytes.Repeat([]byte{0x5a}, 96)}
	mdFrame := rawSection{entry: blockplan.SectionEntry{ID: blockplan.SecMDFrame}, data: []byte{0x00, 0x80, 0x01}}
	instr := rawSection{entry: blockplan.SectionEntry{ID: blockplan.SecInstr}, data: []byte("instructions")}
	pay := rawSection{entry: blockplan.SectionEntry{ID: blockplan.SecPay}, data: []byte("payload")}

	tests := []struct {
		name     string
		sections []rawSection
		err      string
	}{{
		// SEC_PAY is read as a stream from wherever the header and table left
		// the reader, which only works if nothing follows it.
		name:     "a payload that is not the last section",
		sections: []rawSection{pay, sb, mdFrame, instr},
		err:      `SEC_PAY must be the last section, it is entry 1 of 4`,
	}, {
		name:     "one section twice",
		sections: []rawSection{sb, sb, mdFrame, instr},
		err:      `SEC_SB appears twice in the section table`,
	}, {
		name:     "no instruction stream",
		sections: []rawSection{sb, mdFrame},
		err:      `delta is missing SEC_INSTR`,
	}, {
		name:     "no superblock",
		sections: []rawSection{mdFrame, instr},
		err:      `delta is missing SEC_SB`,
	}, {
		// The payload is streamed straight through, so a codec on it would
		// have to be undone in memory -- which is what streaming avoids.
		name: "a compressed payload",
		sections: []rawSection{sb, mdFrame, instr, {
			entry: blockplan.SectionEntry{ID: blockplan.SecPay, Codec: 1}, data: []byte("payload"),
		}},
		err: `SEC_PAY must be stored uncompressed, codec is 1`,
	}, {
		name: "a stored section claiming to expand",
		sections: []rawSection{{
			entry: blockplan.SectionEntry{ID: blockplan.SecSB, RawLen: 999}, data: sb.data,
		}, mdFrame, instr},
		err: `SEC_SB is stored uncompressed but declares raw length 999 for 96 stored bytes`,
	}, {
		// An eager section is read whole into memory, so its declared length
		// has to be bounded before a single byte of it is read.
		name: "an eager section larger than the cap",
		sections: []rawSection{sb, mdFrame, instr, {
			entry: blockplan.SectionEntry{ID: blockplan.SecMDTail, StoredLen: 64<<20 + 1},
		}},
		err: `SEC_MDTAIL is 67108865 bytes, over the 64 MiB cap for an eager section`,
	}, {
		name: "a codec this build has no decoder for",
		sections: []rawSection{sb, mdFrame, instr, {
			entry: blockplan.SectionEntry{ID: blockplan.SecMDTail, Codec: 99, RawLen: 4}, data: []byte("tail"),
		}},
		err: `cannot decompress SEC_MDTAIL: unknown codec 99`,
	}}

	for _, tc := range tests {
		comment := Commentf("%s", tc.name)
		_, err := blockplan.OpenPlan(bytes.NewReader(rawPlan(testPlanHeader(), tc.sections)))
		c.Check(err, ErrorMatches, tc.err, comment)
	}

	// The table these rows are variations on has to open cleanly.
	_, err := blockplan.OpenPlan(bytes.NewReader(rawPlan(testPlanHeader(),
		[]rawSection{sb, mdFrame, instr, pay})))
	c.Check(err, IsNil)
}
