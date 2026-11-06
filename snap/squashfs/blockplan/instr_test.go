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
	"crypto/sha256"
	"math/rand"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type instrSuite struct{}

var _ = Suite(&instrSuite{})

// testSourceSize is deliberately large enough that random source offsets in the
// fuzz test are usually in range, so failures are about encoding rather than
// about bounds rejection.
const testSourceSize = 1 << 30

// testMaxRun is the run cap these tests encode against, and the applier's
// default: it is what bounds how much plaintext one patch run may reconstruct.
const testMaxRun = 8 << 20

// decodeAll walks a whole encoded stream back, which is how the applier reads
// it: one instruction at a time until the buffer is spent.
func decodeAll(c *C, e *blockplan.InstrEncoder, winSizes bool, sourceSize, maxRun int64) []blockplan.Instruction {
	// The decoder's framing must be the encoder's, which is the compressor's.
	d := blockplan.NewInstrDecoder(e.Bytes(), testBlockSize, sourceSize, maxRun, winSizes)
	var out []blockplan.Instruction
	for !d.Done() {
		var in blockplan.Instruction
		c.Assert(d.Next(&in), IsNil, Commentf("decoding instruction %d", len(out)))
		out = append(out, in)
	}
	c.Assert(out, HasLen, e.Count())
	return out
}

// TestRoundTrip covers every opcode and both window framings the encoder can
// produce, including the cases the encoding's invariants turn on: a full block,
// a one-byte block stored raw, the largest partial tail, a plaintext window and
// a patch run with no windows at all.
func (s *instrSuite) TestRoundTrip(c *C) {
	e := blockplan.NewInstrEncoder(testBlockSize, false)

	// A copy from exactly the cursor start, which must encode a zero delta.
	c.Assert(e.Copy(96, 1000), IsNil)
	// Contiguous continuation: also a zero delta.
	c.Assert(e.Copy(1096, 5000), IsNil)
	// Backwards jump: a negative delta, which zigzag must survive.
	c.Assert(e.Copy(200, 7), IsNil)
	c.Assert(e.Literal(4096), IsNil)

	blocks := []blockplan.PlanBlock{
		{USize: testBlockSize, CSize: 1000},     // full block, compressed
		{USize: 7, CSize: 7},                    // tiny tail stored raw
		{USize: testBlockSize - 1, CSize: 4095}, // largest partial tail
		{USize: 1, CSize: 1},                    // one raw byte
	}
	// One compressed window and one already-plaintext window (ULen == Len),
	// which is how a run of raw-stored source blocks travels.
	windows := []blockplan.SrcWindow{
		{Off: 1 << 20, Len: 1 << 16, ULen: 6 << 16},
		{Off: 4096, Len: 100, ULen: 100},
	}
	c.Assert(e.PatchRun(blocks, windows, 12345), IsNil)
	// A patch run with no windows at all: a block built from nothing but the
	// patch, which is how a wholly new file lands here.
	c.Assert(e.PatchRun([]blockplan.PlanBlock{{USize: 99, CSize: 50}}, nil, 60), IsNil)

	got := decodeAll(c, e, false, testSourceSize, testMaxRun)
	want := []blockplan.Instruction{
		{Op: blockplan.OpCopy, SrcOff: 96, Len: 1000},
		{Op: blockplan.OpCopy, SrcOff: 1096, Len: 5000},
		{Op: blockplan.OpCopy, SrcOff: 200, Len: 7},
		{Op: blockplan.OpLiteral, Len: 4096},
		{Op: blockplan.OpPatchRun, Blocks: blocks, Windows: windows, PatchLen: 12345},
		{Op: blockplan.OpPatchRun, Blocks: []blockplan.PlanBlock{{USize: 99, CSize: 50}}, PatchLen: 60},
	}
	c.Assert(got, HasLen, len(want))
	for i := range want {
		comment := Commentf("instruction %d", i)
		// Next reuses slices, so compare field by field with empty and nil
		// treated alike.
		c.Check(got[i].Op, Equals, want[i].Op, comment)
		c.Check(got[i].SrcOff, Equals, want[i].SrcOff, comment)
		c.Check(got[i].Len, Equals, want[i].Len, comment)
		c.Check(got[i].PatchLen, Equals, want[i].PatchLen, comment)
		c.Assert(got[i].Blocks, HasLen, len(want[i].Blocks), comment)
		if len(want[i].Blocks) > 0 {
			c.Check(got[i].Blocks, DeepEquals, want[i].Blocks, comment)
		}
		c.Assert(got[i].Windows, HasLen, len(want[i].Windows), comment)
		if len(want[i].Windows) > 0 {
			c.Check(got[i].Windows, DeepEquals, want[i].Windows, comment)
		}
	}

	// The zero-delta cases must really be one byte each, or the source-cursor
	// scheme is not buying anything.
	e2 := blockplan.NewInstrEncoder(testBlockSize, false)
	c.Assert(e2.Copy(96, 1<<20), IsNil)
	before := len(e2.Bytes())
	c.Assert(e2.Copy(96+(1<<20), 1<<20), IsNil)
	// opcode + one-byte zero delta + three-byte length
	c.Check(len(e2.Bytes())-before, Equals, 5,
		Commentf("a contiguous copy did not encode in the 5 bytes the cursor scheme allows"))
}

// TestRejectsMalformed drives the decoder with streams no encoder produces. On
// the device the stream arrives over the network, so every one of these has to
// be a refusal rather than a decode of whatever the bytes happen to mean.
func (s *instrSuite) TestRejectsMalformed(c *C) {
	tests := []struct {
		name string
		buf  []byte
	}{{
		name: "an opcode of zero",
		buf:  []byte{0},
	}, {
		name: "an opcode this build does not know",
		buf:  []byte{99, 0, 1},
	}, {
		name: "a copy truncated after its opcode",
		buf:  []byte{byte(blockplan.OpCopy)},
	}, {
		name: "a copy of no bytes",
		buf:  []byte{byte(blockplan.OpCopy), 0, 0},
	}, {
		name: "a literal of no bytes",
		buf:  []byte{byte(blockplan.OpLiteral), 0},
	}, {
		name: "a patch run over no blocks",
		buf:  []byte{byte(blockplan.OpPatchRun), 0},
	}, {
		// The declared count must be bounded by the stream that is left, or a
		// single byte could ask the decoder for an arbitrary allocation.
		name: "a patch run whose block count runs past the stream",
		buf:  []byte{byte(blockplan.OpPatchRun), 100},
	}, {
		// A block whose plaintext is exactly the block size must encode as 0,
		// so the long form of the same number is a malformed stream.
		name: "a block size that should have encoded as zero",
		buf: func() []byte {
			e := blockplan.NewInstrEncoder(testBlockSize, false)
			e.PatchRun([]blockplan.PlanBlock{{USize: 4, CSize: 2}}, nil, 0)
			b := append([]byte(nil), e.Bytes()...)
			// Rewrite the encoded uSize from 4 to 0x80 0x80 0x08, which is
			// 131072 the long way round.
			return append(b[:2], append([]byte{0x80, 0x80, 0x08}, b[3:]...)...)
		}(),
	}}

	for _, tc := range tests {
		comment := Commentf("%s", tc.name)
		d := blockplan.NewInstrDecoder(tc.buf, testBlockSize, testSourceSize, testMaxRun, false)
		var in blockplan.Instruction
		var err error
		for !d.Done() && err == nil {
			err = d.Next(&in)
		}
		c.Check(err, NotNil, comment)
	}
}

// TestBoundsChecks covers the three limits that are not formatting rules but
// resource bounds: a reference has to land inside the source image, and both a
// run's plaintext and the windows it is rebuilt from have to stay inside the cap
// the header declares, because the applier holds them in memory at once.
func (s *instrSuite) TestBoundsChecks(c *C) {
	// A copy past the end of the source must be refused even though it
	// encoded fine, because on the device the stream is untrusted.
	e := blockplan.NewInstrEncoder(testBlockSize, false)
	c.Assert(e.Copy(1000, 500), IsNil)
	d := blockplan.NewInstrDecoder(e.Bytes(), testBlockSize, 1200, testMaxRun, false)
	var in blockplan.Instruction
	c.Check(d.Next(&in), ErrorMatches, `copy \[1000,\+500\) is outside the 1200-byte source`)

	// Windows whose plaintext exceeds the cap must be refused: the applier
	// holds all of it in memory at once, so this is a memory bound, not a
	// formatting rule.
	e = blockplan.NewInstrEncoder(testBlockSize, false)
	c.Assert(e.PatchRun([]blockplan.PlanBlock{{USize: 4096, CSize: 100}}, []blockplan.SrcWindow{
		{Off: 0, Len: 1 << 16, ULen: 20 << 20},
	}, 10), IsNil)
	d = blockplan.NewInstrDecoder(e.Bytes(), testBlockSize, testSourceSize, testMaxRun, false)
	c.Check(d.Next(&in), ErrorMatches,
		`patch run windows decompress to 20971520 bytes, over twice the 8388608 cap`)

	// And a patch run reconstructing more plaintext than the cap allows, which
	// is the bound the applier's scratch buffers are sized from.
	e = blockplan.NewInstrEncoder(testBlockSize, false)
	blocks := make([]blockplan.PlanBlock, 100)
	for i := range blocks {
		blocks[i] = blockplan.PlanBlock{USize: testBlockSize, CSize: 100}
	}
	c.Assert(e.PatchRun(blocks, nil, 10), IsNil)
	d = blockplan.NewInstrDecoder(e.Bytes(), testBlockSize, testSourceSize, testMaxRun, false)
	c.Check(d.Next(&in), ErrorMatches, `patch run reconstructs 8519680 bytes, over the 8388608 cap`)
}

// TestMDFrameRoundTrip covers the metadata framing, which is the same two
// invariants over a fixed 8192-byte nominal block, plus the digest that lets the
// applier reject a wrongly patched metadata blob before it writes a data byte.
func (s *instrSuite) TestMDFrameRoundTrip(c *C) {
	blocks := []blockplan.MetaBlock{
		{Offset: 1000, CSize: 4000, USize: blockplan.SquashfsMetadataSize},
		{Offset: 5002, CSize: blockplan.SquashfsMetadataSize, USize: blockplan.SquashfsMetadataSize, Raw: true},
		{Offset: 13196, CSize: 7, USize: 7, Raw: true},
		{Offset: 13205, CSize: 100, USize: 321},
	}
	blob := bytes.Repeat([]byte("metadata"), 100)
	buf, err := blockplan.EncodeMDFrame(blocks, blob)
	c.Assert(err, IsNil)

	got, digest, err := blockplan.DecodeMDFrame(buf, 1000)
	c.Assert(err, IsNil)
	c.Check(got, DeepEquals, blocks, Commentf("the metadata framing did not round trip"))
	c.Check(digest, Equals, sha256.Sum256(blob),
		Commentf("the metadata blob digest did not round trip"))

	// The framing must be compact: a digest plus two varints per block, so a
	// 58-block region stays a few hundred bytes.
	c.Check(len(buf) <= sha256.Size+4*len(blocks), Equals, true,
		Commentf("metadata framing used %d bytes for %d blocks", len(buf), len(blocks)))

	for _, n := range []int{0, 1, sha256.Size - 1, sha256.Size} {
		_, _, err := blockplan.DecodeMDFrame(buf[:n], 1000)
		c.Check(err, NotNil, Commentf("metadata framing truncated to %d bytes was accepted", n))
	}
}

// randomPartition splits n into between one and eight positive parts, which is
// what a window's block sizes have to be: every part at least one byte, and the
// whole summing to the window.
func randomPartition(rng *rand.Rand, n int) []int {
	parts := 1 + rng.Intn(8)
	if parts > n {
		parts = n
	}
	out := make([]int, 0, parts)
	left := n
	for i := 0; i < parts-1; i++ {
		// Leave one byte for each remaining part.
		take := 1 + rng.Intn(left-(parts-i-1))
		out = append(out, take)
		left -= take
	}
	return append(out, left)
}

// FuzzInstrStream spends half its budget on encode/decode symmetry over
// structured input and half on the decoder's tolerance of raw garbage, which is
// what a corrupt or hostile delta looks like from inside the decoder.
func FuzzInstrStream(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	f.Fuzz(func(t *testing.T, seed []byte) {
		var s int64
		for i, b := range seed {
			s = s*31 + int64(b)*int64(i+1)
		}
		rng := rand.New(rand.NewSource(s))
		// Both window framings: a self-delimiting compressor records no block
		// sizes inside a window, one that needs them records every one.
		winSizes := rng.Intn(2) == 0
		e := blockplan.NewInstrEncoder(testBlockSize, winSizes)
		type expect struct {
			op       blockplan.Opcode
			srcOff   int64
			length   int64
			blocks   []blockplan.PlanBlock
			windows  []blockplan.SrcWindow
			patchLen int
		}
		var want []expect
		n := 1 + rng.Intn(200)
		for i := 0; i < n; i++ {
			switch rng.Intn(3) {
			case 0:
				off := rng.Int63n(testSourceSize - 1<<20)
				length := int64(1 + rng.Intn(1<<20))
				if err := e.Copy(off, length); err != nil {
					t.Fatalf("Copy: %v", err)
				}
				want = append(want, expect{op: blockplan.OpCopy, srcOff: off, length: length})
			case 1:
				length := int64(1 + rng.Intn(testMaxRun))
				if err := e.Literal(length); err != nil {
					t.Fatalf("Literal: %v", err)
				}
				want = append(want, expect{op: blockplan.OpLiteral, length: length})
			default:
				nb := 1 + rng.Intn(8)
				blocks := make([]blockplan.PlanBlock, nb)
				for j := range blocks {
					u := 1 + rng.Intn(testBlockSize)
					cs := 1 + rng.Intn(u)
					blocks[j] = blockplan.PlanBlock{USize: u, CSize: cs}
				}
				nw := rng.Intn(4)
				windows := make([]blockplan.SrcWindow, nw)
				// The windows' plaintext must stay inside the decoder's cap of
				// twice the run, so share that budget between them.
				winBudget := 2*testMaxRun/(nw+1) - 1
				for j := range windows {
					length := 1 + rng.Intn(1<<20)
					if length > winBudget {
						length = winBudget
					}
					windows[j] = blockplan.SrcWindow{
						Off: rng.Int63n(testSourceSize - 1<<20),
						Len: length,
						// Anything from plaintext (ULen == Len) up to the
						// budget, so both invariant branches get exercised.
						ULen: length + rng.Intn(winBudget-length+1),
					}
					if winSizes && !windows[j].Plain() {
						windows[j].CSizes = randomPartition(rng, length)
					}
				}
				patchLen := rng.Intn(testMaxRun)
				if err := e.PatchRun(blocks, windows, patchLen); err != nil {
					t.Fatalf("PatchRun: %v", err)
				}
				want = append(want, expect{op: blockplan.OpPatchRun, blocks: blocks, windows: windows, patchLen: patchLen})
			}
		}
		d := blockplan.NewInstrDecoder(e.Bytes(), testBlockSize, testSourceSize, testMaxRun, winSizes)
		for i, wa := range want {
			var in blockplan.Instruction
			if err := d.Next(&in); err != nil {
				t.Fatalf("instruction %d (%v): %v", i, wa.op, err)
			}
			if in.Op != wa.op || in.SrcOff != wa.srcOff || in.Len != wa.length || in.PatchLen != wa.patchLen {
				t.Fatalf("instruction %d: got op=%v src=%d len=%d patch=%d, want op=%v src=%d len=%d patch=%d",
					i, in.Op, in.SrcOff, in.Len, in.PatchLen, wa.op, wa.srcOff, wa.length, wa.patchLen)
			}
			if len(in.Blocks) != len(wa.blocks) {
				t.Fatalf("instruction %d: %d blocks, want %d", i, len(in.Blocks), len(wa.blocks))
			}
			for j := range wa.blocks {
				if in.Blocks[j] != wa.blocks[j] {
					t.Fatalf("instruction %d block %d: got %+v, want %+v", i, j, in.Blocks[j], wa.blocks[j])
				}
			}
			for j := range wa.windows {
				gw, ww := in.Windows[j], wa.windows[j]
				// Next reuses each window's size slice, so an empty one and a
				// nil one mean the same thing.
				if gw.Off != ww.Off || gw.Len != ww.Len || gw.ULen != ww.ULen ||
					len(gw.CSizes) != len(ww.CSizes) {
					t.Fatalf("instruction %d window %d: got %+v, want %+v", i, j, gw, ww)
				}
				for k := range ww.CSizes {
					if gw.CSizes[k] != ww.CSizes[k] {
						t.Fatalf("instruction %d window %d block %d: got %d, want %d",
							i, j, k, gw.CSizes[k], ww.CSizes[k])
					}
				}
			}
		}
		if !d.Done() {
			t.Fatalf("%d bytes left after decoding %d instructions", d.Rest(), len(want))
		}

		// Now the garbage path: whatever the decoder does with the raw
		// seed, it must not panic and must not hand back a reference
		// outside the declared source.
		gd := blockplan.NewInstrDecoder(seed, testBlockSize, 4096, testMaxRun, false)
		for !gd.Done() {
			var in blockplan.Instruction
			if err := gd.Next(&in); err != nil {
				break
			}
			switch in.Op {
			case blockplan.OpCopy:
				if in.SrcOff < 0 || in.SrcOff+in.Len > 4096 {
					t.Fatalf("accepted copy [%d,+%d) outside a 4096-byte source", in.SrcOff, in.Len)
				}
			case blockplan.OpPatchRun:
				if in.USizeTotal() > testMaxRun {
					t.Fatalf("accepted patch run reconstructing %d bytes", in.USizeTotal())
				}
				for _, w := range in.Windows {
					if w.Off < 0 || w.Off+int64(w.Len) > 4096 {
						t.Fatalf("accepted window [%d,+%d) outside a 4096-byte source", w.Off, w.Len)
					}
				}
			}
		}
	})
}
