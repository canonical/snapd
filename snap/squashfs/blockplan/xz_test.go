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
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type xzSuite struct{}

var _ = Suite(&xzSuite{})

// xzStreamFor compresses plaintext into a single .xz stream with a forced block
// boundary at each of blockSizes, the way CompressBlocks does, but with the
// thread count and check under the test's control -- those are exactly the two
// framing decisions the splitter has to police.
func (s *xzSuite) xzStreamFor(c *C, threads int, check string, blockSizes []int, plain []byte) []byte {
	requireTools(c, "xz")
	list := make([]string, len(blockSizes))
	for i, u := range blockSizes {
		list[i] = strconv.Itoa(u)
	}
	cmd, err := blockplan.ToolCommand(context.Background(), "xz",
		"-c", "-q", "--format=xz", "--check="+check,
		fmt.Sprintf("-T%d", threads),
		fmt.Sprintf("--lzma2=preset=6,dict=%d", testBlockSize),
		"--block-list="+strings.Join(list, ","),
		"-")
	c.Assert(err, IsNil)
	cmd.Stdin = bytes.NewReader(plain)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	c.Assert(cmd.Run(), IsNil,
		Commentf("xz -T%d --check=%s failed: %s", threads, check, stderr.String()))
	return out.Bytes()
}

// compressibleBlocks builds plaintext that xz will genuinely shrink, so the
// blocks come back with a compressed size below their uncompressed size and the
// splitter is walking real payloads rather than stored-raw ones.
func compressibleBlocks(sizes []int) []byte {
	var b bytes.Buffer
	for i, n := range sizes {
		pattern := fmt.Sprintf("block %d is filled with repeating text. ", i)
		for b.Len() < cumulative(sizes, i)+n {
			b.WriteString(pattern)
		}
		b.Truncate(cumulative(sizes, i) + n)
	}
	return b.Bytes()
}

func cumulative(sizes []int, upto int) int {
	total := 0
	for _, n := range sizes[:upto] {
		total += n
	}
	return total
}

// crc32OfBlock is the CRC32 of one block's plaintext, which appendXZFrame needs
// in order to rebuild the frame the splitter took apart.
func crc32OfBlock(plain []byte, sizes []int, i int) uint32 {
	start := cumulative(sizes, i)
	return crc32.ChecksumIEEE(plain[start : start+sizes[i]])
}

// TestBlockSplitterWalksStream is the streaming half of the recompression path.
// The splitter consumes a running xz process's stdout, which is what keeps a
// whole image's compressed output from having to be buffered in order to reach
// the index at the end -- so it has to recover every block boundary from the
// headers alone, in order, and stop cleanly at the index.
func (s *xzSuite) TestBlockSplitterWalksStream(c *C) {
	sizes := []int{4000, testBlockSize, 777}
	plain := compressibleBlocks(sizes)
	stream := s.xzStreamFor(c, 2, "crc32", sizes, plain)

	split, err := blockplan.NewXZBlockSplitter(bytes.NewReader(stream))
	c.Assert(err, IsNil)

	var payloads [][]byte
	for i := 0; ; i++ {
		payload, uSize, err := split.Next()
		if err == io.EOF {
			break
		}
		c.Assert(err, IsNil, Commentf("block %d", i))
		c.Assert(i < len(sizes), Equals, true,
			Commentf("the splitter produced more than the %d blocks xz was asked for", len(sizes)))
		// -T2 uses the buffer encoder, whose headers carry both sizes. The
		// declared uncompressed size is what CompressBlocks cross-checks its
		// own block list against, so a splitter that lost it would disable
		// that check.
		c.Check(uSize, Equals, sizes[i], Commentf("block %d's declared uncompressed size", i))
		c.Check(len(payload) > 0, Equals, true, Commentf("block %d has an empty payload", i))
		c.Check(len(payload) < sizes[i], Equals, true,
			Commentf("block %d payload is %d bytes against %d of plaintext, so it did not compress",
				i, len(payload), sizes[i]))
		// The slice is documented as valid only until the next call, so copy.
		payloads = append(payloads, append([]byte(nil), payload...))
	}
	c.Assert(payloads, HasLen, len(sizes))

	// Once EOF is reached it must stay reached, because the caller loops on it.
	_, _, err = split.Next()
	c.Check(err, Equals, io.EOF)

	// The payloads have to be the real LZMA2 data: framed individually they
	// must decompress back to their own slice of the plaintext. This is what
	// makes the forward walk equivalent to compressing each block on its own.
	for i, payload := range payloads {
		comment := Commentf("block %d", i)
		framed, err := blockplan.AppendXZFrame(nil, payload, sizes[i], crc32OfBlock(plain, sizes, i), testBlockSize)
		c.Assert(err, IsNil, comment)
		got, err := blockplan.XZDecompressAll(context.Background(), framed, sizes[i])
		c.Assert(err, IsNil, comment)
		want := plain[cumulative(sizes, i) : cumulative(sizes, i)+sizes[i]]
		c.Check(bytes.Equal(got, want), Equals, true,
			Commentf("block %d did not round-trip through the splitter, first difference at %d",
				i, firstDiff(got, want)))
	}
}

// The refusals below exist so the splitter never has to buffer. Each of these
// would otherwise surface as a corrupt image rather than a clean failure.

// This is the reason threadArg() has a floor of 2. -T1 uses the streaming
// encoder, whose block headers omit the compressed size, so there is no way to
// find the next boundary without reading the index at the end of the stream --
// which a reader consuming a running xz has not got yet.
func (s *xzSuite) TestBlockSplitterRefusesSingleThreadedFraming(c *C) {
	sizes := []int{4000, 4000}
	stream := s.xzStreamFor(c, 1, "crc32", sizes, compressibleBlocks(sizes))

	// The stream header itself is fine, so opening it succeeds.
	split, err := blockplan.NewXZBlockSplitter(bytes.NewReader(stream))
	c.Assert(err, IsNil)
	// The message has to name the remedy: this failure is only ever a
	// misconfigured thread count.
	_, _, err = split.Next()
	c.Check(err, ErrorMatches, `xz block header omits the compressed size \(flags 0x00\); run xz with -T2 or higher`)
}

// squashfs blocks carry CRC32. Accepting another check would produce frames that
// mksquashfs never emits, so the image would differ from the target.
func (s *xzSuite) TestBlockSplitterRefusesTheWrongCheckType(c *C) {
	sizes := []int{4000, 4000}
	stream := s.xzStreamFor(c, 2, "crc64", sizes, compressibleBlocks(sizes))

	_, err := blockplan.NewXZBlockSplitter(bytes.NewReader(stream))
	c.Check(err, ErrorMatches, `xz stream uses check 0x4, want CRC32`)
}

func (s *xzSuite) TestBlockSplitterRefusesNonXZInput(c *C) {
	_, err := blockplan.NewXZBlockSplitter(strings.NewReader("this is not compressed at all!!"))
	c.Check(err, ErrorMatches, `not an xz stream \(magic 746869732069\)`)
}

func (s *xzSuite) TestBlockSplitterRefusesATruncatedStreamHeader(c *C) {
	sizes := []int{4000, 4000}
	stream := s.xzStreamFor(c, 2, "crc32", sizes, compressibleBlocks(sizes))

	_, err := blockplan.NewXZBlockSplitter(bytes.NewReader(stream[:8]))
	c.Check(err, ErrorMatches, `cannot read xz stream header: unexpected EOF`)
}

// A short read inside a payload must be an error rather than a short payload,
// because a silently truncated block would be framed and written into the image.
// The cut has to land inside the payload: everything past the last block is the
// index and the footer, which the splitter never reads -- it stops at the index
// indicator, and a stream that ends early there is caught by the caller's own
// block count instead.
func (s *xzSuite) TestBlockSplitterRefusesTruncationMidPayload(c *C) {
	sizes := []int{4000}
	stream := s.xzStreamFor(c, 2, "crc32", sizes, compressibleBlocks(sizes))

	split, err := blockplan.NewXZBlockSplitter(bytes.NewReader(stream))
	c.Assert(err, IsNil)
	payload, _, err := split.Next()
	c.Assert(err, IsNil)

	// Stream header, then the block header sized for this block, then half the
	// payload: a boundary the splitter has committed to reading past.
	cut := 12 + blockplan.XZBlockHeaderSize(sizes[0]) + len(payload)/2
	c.Assert(cut < len(stream), Equals, true,
		Commentf("the cut at %d is not inside a %d-byte stream", cut, len(stream)))

	split, err = blockplan.NewXZBlockSplitter(bytes.NewReader(stream[:cut]))
	c.Assert(err, IsNil)
	_, _, err = split.Next()
	c.Check(err, ErrorMatches, `cannot read xz block payload: unexpected EOF`)
}
