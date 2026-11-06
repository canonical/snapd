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
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/testutil"
)

type patchtoolSuite struct {
	testutil.BaseTest
}

var _ = Suite(&patchtoolSuite{})

func (s *patchtoolSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Point the system snap at an empty tree so that the mocked tools below are
	// reached through $PATH, and so that a machine with a snapd snap installed
	// does not run the tools from it.
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *patchtoolSuite) mockTool(c *C, tool, script string) *testutil.MockCmd {
	mock := testutil.MockCommand(c, tool, script)
	s.AddCleanup(mock.Restore)
	return mock
}

// patchPair is a plaintext and a changed version of it, the shape a patch run
// actually sees: mostly the same bytes, with an edit in the middle and a
// different tail.
func patchPair() (old, updated []byte) {
	old = compressibleText(300000, "before")
	updated = append([]byte(nil), old...)
	copy(updated[150000:], compressibleText(20000, "edited"))
	return old, append(updated, compressibleText(5000, "appended")...)
}

// TestPatchRoundTrip is the pair the format is named after, driven the way a
// patch run drives it: hdiffz makes a patch from two plaintexts, hpatchz rebuilds
// the second from the first and the patch, and the result has to be the same
// bytes -- a patch run that is a byte out is a corrupt image.
func (s *patchtoolSuite) TestPatchRoundTrip(c *C) {
	requireTools(c, "hdiffz", "hpatchz")
	ctx := context.Background()
	old, updated := patchPair()

	patch, err := blockplan.RunHdiffz(ctx, old, updated, nil)
	c.Assert(err, IsNil)
	// The patch has to be far smaller than the plaintext it rebuilds, or the
	// run costing that chooses patch runs over literals is choosing on a false
	// premise.
	c.Check(len(patch) < len(updated)/10, Equals, true,
		Commentf("hdiffz made a %d-byte patch for %d bytes of plaintext", len(patch), len(updated)))

	got, err := blockplan.RunHpatchz(ctx, old, patch, int64(len(updated)))
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(got, updated), Equals, true,
		Commentf("the patch did not rebuild the plaintext, first difference at %d", firstDiff(got, updated)))

	// A run with no source at all: a wholly new file has no window to patch
	// against, so the patch carries all of it.
	patch, err = blockplan.RunHdiffz(ctx, nil, updated, nil)
	c.Assert(err, IsNil)
	got, err = blockplan.RunHpatchz(ctx, nil, patch, int64(len(updated)))
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(got, updated), Equals, true,
		Commentf("a patch from an empty source did not rebuild the plaintext, first difference at %d",
			firstDiff(got, updated)))
}

// TestPatchIntoScratchFiles is the same round trip through the entry point an
// apply uses, which keeps the plaintext in a file rather than in the heap, and
// reuses the same three files for every run of the whole delta.
func (s *patchtoolSuite) TestPatchIntoScratchFiles(c *C) {
	requireTools(c, "hdiffz", "hpatchz")
	ctx := context.Background()
	old, updated := patchPair()

	patch, err := blockplan.RunHdiffz(ctx, old, updated, nil)
	c.Assert(err, IsNil)

	oldFile, err := blockplan.NewMemFileWith("old", old)
	c.Assert(err, IsNil)
	defer oldFile.Close()
	patchFile, err := blockplan.NewMemFileWith("patch", patch)
	c.Assert(err, IsNil)
	defer patchFile.Close()
	outFile, err := blockplan.NewMemFile("new")
	c.Assert(err, IsNil)
	defer outFile.Close()

	readOut := func() []byte {
		_, err := outFile.File().Seek(0, io.SeekStart)
		c.Assert(err, IsNil)
		out, err := io.ReadAll(outFile.File())
		c.Assert(err, IsNil)
		return out
	}

	c.Assert(blockplan.RunHpatchzFiles(ctx, oldFile, patchFile, outFile, int64(len(updated))), IsNil)
	got := readOut()
	c.Check(bytes.Equal(got, updated), Equals, true,
		Commentf("the patch did not rebuild the plaintext, first difference at %d", firstDiff(got, updated)))

	// The second run over the same files is the one that matters: an apply has
	// as many runs as the image has changed regions, and every one of them
	// rewrites these three files. A shorter result must not leave the previous
	// run's tail behind it.
	shortPatch, err := blockplan.RunHdiffz(ctx, old, old[:1000], nil)
	c.Assert(err, IsNil)
	c.Assert(patchFile.Reset(), IsNil)
	_, err = patchFile.File().Write(shortPatch)
	c.Assert(err, IsNil)

	c.Assert(blockplan.RunHpatchzFiles(ctx, oldFile, patchFile, outFile, 1000), IsNil)
	got = readOut()
	c.Check(bytes.Equal(got, old[:1000]), Equals, true,
		Commentf("reusing the scratch files left %d bytes behind", len(got)-1000))
}

// TestToolArgs pins the tuning and the argument order. The tuning is measured --
// see the comments on it -- so a change to it should show up here as a failing
// test rather than as a slower generate or a larger patch, and the three paths
// are positional, where swapping two of them makes hpatchz rebuild nonsense
// rather than fail.
func (s *patchtoolSuite) TestToolArgs(c *C) {
	ctx := context.Background()

	// The stand-in concatenates the two inputs into the output, so what comes
	// back proves which path each argument was.
	hdiffz := s.mockTool(c, "hdiffz", `cat "$6" "$7" > "$8"`)
	out, err := blockplan.RunHdiffz(ctx, []byte("the source"), []byte("the target"), nil)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "the sourcethe target")

	calls := hdiffz.Calls()
	c.Assert(calls, HasLen, 1)
	c.Check(calls[0][:6], DeepEquals, []string{"hdiffz", "-m-6", "-SD", "-c-zstd-21-24", "-d", "-f"})
	c.Check(calls[0][6:], HasLen, 3, Commentf("hdiffz was not given exactly source, target and patch"))

	hpatchz := s.mockTool(c, "hpatchz", `cat "$3" "$4" > "$5"`)
	got, err := blockplan.RunHpatchz(ctx, []byte("the source"), []byte("the patch"), int64(len("the sourcethe patch")))
	c.Assert(err, IsNil)
	c.Check(string(got), Equals, "the sourcethe patch")

	calls = hpatchz.Calls()
	c.Assert(calls, HasLen, 1)
	c.Check(calls[0][:3], DeepEquals, []string{"hpatchz", "-s-8m", "-f"})
	c.Check(calls[0][3:], HasLen, 3, Commentf("hpatchz was not given exactly source, patch and output"))
}

// TestToolFailuresAreReported covers what a caller has to be told. Both tools
// say why they failed on stderr, and dropping that leaves "exit status 1" as the
// only explanation of a refused delta.
func (s *patchtoolSuite) TestToolFailuresAreReported(c *C) {
	ctx := context.Background()

	s.mockTool(c, "hdiffz", "echo 'hdiffz: options -c- ERROR!' >&2; exit 2")
	_, err := blockplan.RunHdiffz(ctx, []byte("source"), []byte("target"), nil)
	c.Check(err, ErrorMatches, `hdiffz failed: exit status 2: hdiffz: options -c- ERROR!`)

	s.mockTool(c, "hpatchz", "echo 'patch file is not a hdiffz patch' >&2; exit 1")
	_, err = blockplan.RunHpatchz(ctx, []byte("source"), []byte("not a patch"), 6)
	c.Check(err, ErrorMatches, `hpatchz failed: exit status 1: patch file is not a hdiffz patch`)
}

// TestHpatchzLengthIsChecked is the one thing about a reconstructed run that can
// be checked before its bytes are used: the delta says how much plaintext the run
// is, and a patch that produces anything else has already gone wrong.
func (s *patchtoolSuite) TestHpatchzLengthIsChecked(c *C) {
	s.mockTool(c, "hpatchz", `printf 'abc' > "$5"`)

	_, err := blockplan.RunHpatchz(context.Background(), []byte("source"), []byte("patch"), 10)
	c.Check(err, ErrorMatches, `the patch produced 3 bytes, the delta says the run is 10`)
}

// TestHdiffzArgsReplaceCollidingOption pins the merge that makes extra hdiffz
// options usable at all: hdiffz aborts on an option given twice, so an extra
// has to replace the built-in setting of the same option rather than join it.
func (s *patchtoolSuite) TestHdiffzArgsReplaceCollidingOption(c *C) {
	tuning := blockplan.HdiffzTuning()
	c.Assert(tuning, DeepEquals, []string{"-m-6", "-SD", "-c-zstd-21-24", "-d"},
		Commentf("the cases below are written against this tuning"))

	for _, tc := range []struct {
		summary string
		extra   []string
		want    []string
	}{{
		summary: "no extras leaves the tuning alone",
		extra:   nil,
		want:    []string{"-m-6", "-SD", "-c-zstd-21-24", "-d"},
	}, {
		summary: "an extra naming a set option replaces it, in place of joining it",
		extra:   []string{"-c-zstd-19-24"},
		want:    []string{"-m-6", "-SD", "-d", "-c-zstd-19-24"},
	}, {
		summary: "an option the tuning does not set is simply added",
		extra:   []string{"-block-0"},
		want:    []string{"-m-6", "-SD", "-c-zstd-21-24", "-d", "-block-0"},
	}, {
		summary: "replacement and addition together",
		extra:   []string{"-c-zstd-19-24", "-block-0"},
		want:    []string{"-m-6", "-SD", "-d", "-c-zstd-19-24", "-block-0"},
	}, {
		summary: "case is part of the option name: -C is not -c",
		extra:   []string{"-C-crc32"},
		want:    []string{"-m-6", "-SD", "-c-zstd-21-24", "-d", "-C-crc32"},
	}} {
		c.Check(blockplan.HdiffzArgs(tc.extra), DeepEquals, tc.want, Commentf("%s", tc.summary))
	}
}

// TestParseHdiffzArgsRefusesNonOptions checks that a bare word is refused.
// hdiffz reads its old, new and diff paths positionally, so a word smuggled
// into the options would take the place of one of them and hdiffz would either
// diff the wrong file or write the diff over it.
func (s *patchtoolSuite) TestParseHdiffzArgsRefusesNonOptions(c *C) {
	for _, tc := range []struct {
		in   string
		want []string
		err  string
	}{
		{in: "", want: []string{}},
		{in: "   ", want: []string{}},
		{in: "-m-4", want: []string{"-m-4"}},
		{in: "  -m-4   -d  ", want: []string{"-m-4", "-d"}},
		{in: "-m-4 oops", err: `extra hdiffz options are each a dash and an option name: "oops" is not one`},
		{in: "/etc/passwd", err: `.*: "/etc/passwd" is not one`},
		{in: "-", err: `.*: "-" is not one`},
		{in: "--", err: `.*: "--" is not one`},
	} {
		got, err := blockplan.ParseHdiffzArgs(tc.in)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, Commentf("input %q", tc.in))
			c.Check(got, IsNil, Commentf("input %q", tc.in))
			continue
		}
		c.Check(err, IsNil, Commentf("input %q", tc.in))
		c.Check(got, DeepEquals, tc.want, Commentf("input %q", tc.in))
	}
}
