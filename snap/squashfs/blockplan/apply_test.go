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
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type applySuite struct{}

var _ = Suite(&applySuite{})

// The applier is what runs on the device, so what these tests hold is not only
// that a delta reconstructs its target but that a bad one is refused before any
// of the image exists. A refusal costs a full download; a plausible-looking wrong
// image costs a broken snap, and nothing downstream would notice.

// countingWriter records how much was written without keeping it, so a test can
// tell whether a failure happened before any target bytes were produced.
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// sectionOffsets maps each section id to where its stored bytes begin, and to its
// table entry offset, so a test can rewrite a section in place.
func sectionOffsets(c *C, delta []byte) (payload, entry map[uint16]int, entries map[uint16]blockplan.SectionEntry) {
	h, err := blockplan.ParsePlanHeader(delta)
	c.Assert(err, IsNil)
	payload, entry, entries = map[uint16]int{}, map[uint16]int{}, map[uint16]blockplan.SectionEntry{}
	at := blockplan.PlanHeaderSize + int(h.SectionCount)*blockplan.SectionEntrySize
	for i := 0; i < int(h.SectionCount); i++ {
		eoff := blockplan.PlanHeaderSize + i*blockplan.SectionEntrySize
		e := blockplan.ParseSectionEntry(delta[eoff:])
		payload[e.ID], entry[e.ID], entries[e.ID] = at, eoff, e
		at += int(e.StoredLen)
	}
	c.Assert(at, Equals, len(delta),
		Commentf("section payloads end at %d but the delta is %d bytes", at, len(delta)))
	return payload, entry, entries
}

// corruptSection flips one bit inside a section's stored bytes and repairs the
// section's CRC, so the container stays well-formed. Without the repair every
// such test would merely prove that CRC32 works; with it, whatever rejects the
// delta is the check under test.
func corruptSection(c *C, delta []byte, id uint16, at int) []byte {
	bad := append([]byte(nil), delta...)
	payload, entry, entries := sectionOffsets(c, bad)
	e, ok := entries[id]
	c.Assert(ok, Equals, true, Commentf("the delta has no %s to corrupt", blockplan.SectionName(id)))
	c.Assert(at < int(e.StoredLen), Equals, true,
		Commentf("%s is %d bytes, cannot corrupt offset %d", blockplan.SectionName(id), e.StoredLen, at))
	start := payload[id]
	bad[start+at] ^= 0x01
	e.CRC = crc32.ChecksumIEEE(bad[start : start+int(e.StoredLen)])
	copy(bad[entry[id]:], e.Marshal())
	return bad
}

// applyBytes applies a whole delta held in memory, reporting how much of the
// image was produced -- because for a refusal the point is that it was none.
func applyBytes(c *C, source string, delta []byte) (int64, error) {
	src, err := os.Open(source)
	c.Assert(err, IsNil)
	defer src.Close()

	var out countingWriter
	_, err = blockplan.Apply(context.Background(), src, bytes.NewReader(delta), &out,
		&blockplan.ApplyOpts{Comp: newXZ(c)})
	return out.n, err
}

// TestRoundTripsRealImages is the format's premise end to end: most of the image
// arrives without the compressor, and the only plaintext pushed through it is
// metadata.
func (s *applySuite) TestRoundTripsRealImages(c *C) {
	source, target, delta := deltaFixture(c, smallEdit)

	stats := applyAndCompare(c, source, delta, target, newXZ(c))
	c.Check(stats.CopiedBytes > 0, Equals, true,
		Commentf("nothing was copied verbatim, so no CPU was saved"))
	c.Check(stats.UCompressedBytes, Equals, stats.MetaUBytes,
		Commentf("compressed %d bytes of plaintext but only %d were metadata; with no patch runs they must agree",
			stats.UCompressedBytes, stats.MetaUBytes))
}

// TestRejectsCorruptMDPatch is the metadata gate: a delta whose metadata patch
// has been tampered with must be refused, and refused before any of the target is
// produced. That ordering is the reason the metadata sections come first in the
// format.
func (s *applySuite) TestRejectsCorruptMDPatch(c *C) {
	source, _, delta := deltaFixture(c, smallEdit)

	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	_, _, entries := sectionOffsets(c, whole)
	patch, ok := entries[blockplan.SecMDPatch]
	c.Assert(ok, Equals, true, Commentf("the fixture produced no SEC_MDPATCH, so there is nothing to corrupt"))
	c.Assert(patch.StoredLen > 0, Equals, true, Commentf("SEC_MDPATCH is empty"))

	// Walk a spread of offsets: patch headers, control data and literals all
	// behave differently under a bit flip.
	for _, at := range []int{0, 1, int(patch.StoredLen) / 4, int(patch.StoredLen) / 2, int(patch.StoredLen) - 1} {
		cmt := Commentf("SEC_MDPATCH corrupted at byte %d", at)
		n, err := applyBytes(c, source, corruptSection(c, whole, blockplan.SecMDPatch, at))
		if !c.Check(err, NotNil, cmt) {
			continue
		}
		c.Check(n, Equals, int64(0),
			Commentf("byte %d: the corrupt patch was only caught after %d bytes of the target had been written: %v",
				at, n, err))
	}
}

// TestRejectsCorruptMDFrame covers the other half of the metadata gate: the
// framing itself, including the blob digest that makes the patch check possible
// before any data work.
func (s *applySuite) TestRejectsCorruptMDFrame(c *C) {
	source, _, delta := deltaFixture(c, smallEdit)

	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	_, _, entries := sectionOffsets(c, whole)
	frame := entries[blockplan.SecMDFrame]

	for _, t := range []struct {
		at int
		// wantErr, when set, is text the error must contain, so the test
		// proves which check fired rather than only that one did.
		wantErr string
	}{
		// The first and last bytes of the digest. Nothing else can catch
		// these: the patch is untouched, so it applies cleanly and produces a
		// blob of exactly the right length.
		{at: 0, wantErr: "metadata digest"},
		{at: sha256.Size - 1, wantErr: "metadata digest"},
		// The framing proper: a uSize and a cSize varint.
		{at: sha256.Size},
		{at: int(frame.StoredLen) - 1},
	} {
		cmt := Commentf("SEC_MDFRAME corrupted at byte %d", t.at)
		n, err := applyBytes(c, source, corruptSection(c, whole, blockplan.SecMDFrame, t.at))
		if !c.Check(err, NotNil, cmt) {
			continue
		}
		if t.wantErr != "" {
			c.Check(err, ErrorMatches, ".*"+t.wantErr+".*",
				Commentf("byte %d: expected the %q check to fire", t.at, t.wantErr))
		}
		c.Check(n, Equals, int64(0),
			Commentf("byte %d: corrupt framing was only caught after %d bytes had been written: %v",
				t.at, n, err))
	}
}

// TestRejectsWrongSource proves the source digest is what makes every OP_COPY
// safe: applied against the wrong revision, the delta must be refused outright
// rather than producing a plausible-looking image.
func (s *applySuite) TestRejectsWrongSource(c *C) {
	_, target, delta := deltaFixture(c, smallEdit)

	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	// The target stands in for "some other revision": it is the one other image
	// here that a device could plausibly hold.
	n, err := applyBytes(c, target, whole)
	c.Assert(err, NotNil, Commentf("the delta was applied to the wrong source image"))
	c.Check(n, Equals, int64(0),
		Commentf("the wrong source was only caught after %d bytes had been written: %v", n, err))
}

// TestRejectsCorruptPayload checks the one section whose CRC cannot be verified at
// open time, because it is never held whole. A literal block's bytes go straight
// to the target, so this is caught by the payload CRC and the image digest rather
// than up front -- which is exactly why both exist.
func (s *applySuite) TestRejectsCorruptPayload(c *C) {
	source, _, delta := deltaFixture(c, smallEdit)

	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	_, _, entries := sectionOffsets(c, whole)
	pay, ok := entries[blockplan.SecPay]
	c.Assert(ok && pay.StoredLen > 0, Equals, true,
		Commentf("the fixture produced no SEC_PAY, so there is nothing to corrupt"))

	_, err = applyBytes(c, source, corruptSection(c, whole, blockplan.SecPay, int(pay.StoredLen)/2))
	c.Check(err, NotNil, Commentf("a corrupt SEC_PAY was accepted"))
}

// TestCanaryDetectsToolchainDrift proves SEC_CANARY does its job: a delta whose
// canary was recorded by a different compressor configuration must be refused
// before the target is created, which is the difference between a clean fall-back
// and a corrupt image.
func (s *applySuite) TestCanaryDetectsToolchainDrift(c *C) {
	source, _, delta := deltaFixture(c, smallEdit)

	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	// Rewrite the canary's recorded compressed length for the data
	// configuration, which is what a drifting liblzma would change.
	payload, entry, entries := sectionOffsets(c, whole)
	e := entries[blockplan.SecCanary]
	bad := append([]byte(nil), whole...)
	start := payload[blockplan.SecCanary]
	binary.LittleEndian.PutUint32(bad[start+4:], binary.LittleEndian.Uint32(bad[start+4:])+1)
	e.CRC = crc32.ChecksumIEEE(bad[start : start+int(e.StoredLen)])
	copy(bad[entry[blockplan.SecCanary]:], e.Marshal())

	n, err := applyBytes(c, source, bad)
	c.Assert(err, NotNil, Commentf("a delta recording different compressor output was accepted"))
	c.Check(n, Equals, int64(0), Commentf("the canary fired only after %d bytes had been written", n))
}

// applyWithBudget applies a delta under a stated memory budget, returning what it
// managed to write as well as the outcome -- because for a refusal the point is
// that it wrote nothing.
func applyWithBudget(c *C, source, delta string, budget int) (*blockplan.ApplyStats, []byte, error) {
	src, err := os.Open(source)
	c.Assert(err, IsNil)
	defer src.Close()
	df, err := os.Open(delta)
	c.Assert(err, IsNil)
	defer df.Close()

	var got bytes.Buffer
	stats, err := blockplan.Apply(context.Background(), src, df, &got, &blockplan.ApplyOpts{
		Comp:        newXZ(c),
		MaxRunUSize: budget,
	})
	return stats, got.Bytes(), err
}

// TestNegotiatesRunCap is the trade the format offers a device with a memory
// budget. A patch run needs its plaintext and its source window in scratch at
// once, so the header's run cap sets the whole apply's memory demand -- and an
// applier that cannot afford what a delta declares must say so before reading,
// writing or forking anything. Falling back to a full download is a far better
// outcome than failing part-way through assembling an image.
func (s *applySuite) TestNegotiatesRunCap(c *C) {
	source, target := churnPair(c)
	delta := filepath.Join(c.MkDir(), "negotiated.delta")

	const cap4 = 4 * testBlockSize
	gen, err := blockplan.Generate(context.Background(), source, target, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true, MaxRunUSize: cap4,
	})
	c.Assert(err, IsNil, Commentf("generating under a %d-byte run cap", cap4))
	c.Assert(gen.PatchRuns > 0, Equals, true,
		Commentf("no patch run was emitted, so there is no memory demand to negotiate over"))

	// A budget one byte below what the delta declares must refuse, and must not
	// have written any of the image on the way to finding out.
	_, out, err := applyWithBudget(c, source, delta, cap4-1)
	c.Assert(err, NotNil,
		Commentf("a delta declaring a %d-byte run cap was accepted by an applier allowing %d", cap4, cap4-1))
	c.Check(out, HasLen, 0,
		Commentf("the refusal still wrote %d bytes of image, so it was not decided up front", len(out)))
	// Both numbers have to appear: which side was too small is what decides the
	// caller's next move.
	c.Check(err.Error(), Matches, ".*"+blockplan.HumanBytes(cap4)+".*")
	c.Check(err.Error(), Matches, ".*"+blockplan.HumanBytes(cap4-1)+".*")

	// A budget exactly at the declared cap is affordable, and the scratch the
	// apply actually used has to come in under it -- otherwise the number the
	// negotiation is conducted in does not bound anything.
	st, got, err := applyWithBudget(c, source, delta, cap4)
	c.Assert(err, IsNil,
		Commentf("a delta was refused by an applier whose budget exactly matches it"))
	want, err := os.ReadFile(target)
	c.Assert(err, IsNil)
	c.Assert(bytes.Equal(got, want), Equals, true,
		Commentf("the reconstruction differs from the target at offset %d", firstDiff(got, want)))
	c.Assert(st.PeakScratchBytes > 0, Equals, true,
		Commentf("a delta with patch runs reported no scratch, so the measurement is not wired up"))
	// The decoder bounds a run at the cap and its windows at twice that, and the
	// patch cannot exceed the delta that carries it. Those three are what the
	// scratch files hold at once.
	bound := int64(3*cap4) + gen.DeltaSize
	c.Check(st.PeakScratchBytes <= bound, Equals, true,
		Commentf("one run held %s of scratch under a %s cap, above the %s the format bounds it to",
			blockplan.HumanBytes(st.PeakScratchBytes), blockplan.HumanBytes(cap4), blockplan.HumanBytes(bound)))

	// Zero means "whatever the delta asks for", which is what a caller with no
	// budget of its own wants.
	_, _, err = applyWithBudget(c, source, delta, 0)
	c.Check(err, IsNil, Commentf("an applier with no stated budget refused a delta"))
}

// TestRunCapBoundsScratch is the other half: the cap is not merely declared and
// checked, it is what the apply's peak memory actually follows. Halving it has to
// halve what one run holds, or the negotiation is theatre.
func (s *applySuite) TestRunCapBoundsScratch(c *C) {
	source, target := churnPair(c)
	dir := c.MkDir()

	scratchAt := func(runCap int) int64 {
		delta := filepath.Join(dir, "cap.delta")
		_, err := blockplan.Generate(context.Background(), source, target, delta, &blockplan.GenerateOpts{
			Comp: newXZ(c), MaxRunUSize: runCap,
		})
		c.Assert(err, IsNil, Commentf("generating under a %d-byte run cap", runCap))
		st, got, err := applyWithBudget(c, source, delta, runCap)
		c.Assert(err, IsNil, Commentf("applying under a %d-byte run cap", runCap))
		want, err := os.ReadFile(target)
		c.Assert(err, IsNil)
		c.Assert(bytes.Equal(got, want), Equals, true,
			Commentf("a %d-byte cap reconstructed the wrong image at offset %d", runCap, firstDiff(got, want)))
		return st.PeakScratchBytes
	}

	wide := scratchAt(8 * testBlockSize)
	narrow := scratchAt(2 * testBlockSize)
	c.Check(narrow < wide, Equals, true,
		Commentf("a two-block cap held %s of scratch against %s for an eight-block cap, "+
			"so the cap does not bound memory", blockplan.HumanBytes(narrow), blockplan.HumanBytes(wide)))
}

// TestApplyToFile is the entry point snapd itself calls: the same reconstruction,
// written to a path rather than a stream. A failed apply is reported as an error
// and leaves nothing at the target path, because the image is assembled beside
// it and renamed only once the apply has succeeded.
func (s *applySuite) TestApplyToFile(c *C) {
	source, target, delta := deltaFixture(c, smallEdit)
	out := filepath.Join(c.MkDir(), "reconstructed.snap")

	df, err := os.Open(delta)
	c.Assert(err, IsNil)
	defer df.Close()
	_, err = blockplan.ApplyToFile(context.Background(), source, df, out, nil)
	c.Assert(err, IsNil)

	got, err := os.ReadFile(out)
	c.Assert(err, IsNil)
	want, err := os.ReadFile(target)
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(got, want), Equals, true,
		Commentf("the reconstruction differs from the target at offset %d", firstDiff(got, want)))

	// Applied to the wrong source it must fail, and must leave nothing behind
	// at the target path: not a partial image, and not an empty file either.
	whole, err := os.ReadFile(delta)
	c.Assert(err, IsNil)
	dir := c.MkDir()
	refused := filepath.Join(dir, "refused.snap")
	_, err = blockplan.ApplyToFile(context.Background(), target, bytes.NewReader(whole), refused, nil)
	c.Assert(err, NotNil, Commentf("applying to the wrong source succeeded"))
	_, err = os.Stat(refused)
	c.Check(os.IsNotExist(err), Equals, true,
		Commentf("a refused apply left something at the target path: %v", err))
	left, err := filepath.Glob(filepath.Join(dir, "*"))
	c.Assert(err, IsNil)
	c.Check(left, HasLen, 0, Commentf("a refused apply left scratch behind: %q", left))

	// The case that makes the rename worth having: a caller pointing at the
	// snap it is replacing still has it after a refusal. Truncating it here
	// would destroy the revision the device is running from.
	keep := filepath.Join(c.MkDir(), "existing.snap")
	c.Assert(os.WriteFile(keep, []byte("the revision already installed"), 0644), IsNil)
	_, err = blockplan.ApplyToFile(context.Background(), target, bytes.NewReader(whole), keep, nil)
	c.Assert(err, NotNil)
	kept, err := os.ReadFile(keep)
	c.Assert(err, IsNil)
	c.Check(string(kept), Equals, "the revision already installed",
		Commentf("a refused apply overwrote an existing target"))
}
