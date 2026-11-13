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

package blockplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// This file drives the patch tool the format's name ends in: hdiffz makes the
// patches when a delta is generated, hpatchz applies them on the device. Only
// hpatchz is needed to apply, which is what commits the snapd snap to shipping
// one binary rather than two.
//
// Both take their inputs and their output as paths rather than as descriptors,
// so every call here hands them scratch files -- memfds in practice, see
// memfd.go -- and a patch run's plaintext never has to exist in the heap.

var (
	// hdiffzTuning is what a patch is made with. -m-6 raises the match score
	// to 6 from the -m-4 default: hdiffz recommends 0--4 for binary data and
	// 4--9 for text, but on these images the higher score measured about 2x
	// faster at equal patch size. -SD is the single-compressed diff format,
	// which costs the device one decompress buffer, at the default 256k step.
	// -c-zstd-21-24 compresses the diff with zstd level 21 and a 16 MiB
	// window. -d skips hdiffz's own patch check, which the generator repeats
	// byte for byte anyway.
	hdiffzTuning = []string{"-m-6", "-SD", "-c-zstd-21-24", "-d"}

	// hpatchzTuning names the 8 MiB stream cache, which is already hpatchz's
	// default, so that the memory an apply asks for is stated rather than
	// inherited. Call sites add -f, force overwrite, because the file they
	// write to is a scratch file they own.
	hpatchzTuning = []string{"-s-8m"}
)

// hdiffzOption is the option an argument names, without its value, because
// hdiffz spells the value into the option itself: -c-zstd-21-24 and
// -c-zstd-19-24 are one option set twice, not two options. Case is part of the
// name -- -c compresses the diff, -C picks its checksum.
func hdiffzOption(arg string) string {
	name := strings.TrimPrefix(arg, "-")
	end := 0
	for end < len(name) && (name[end] >= 'a' && name[end] <= 'z' || name[end] >= 'A' && name[end] <= 'Z') {
		end++
	}
	return "-" + name[:end]
}

// hdiffzArgs merges extra options into the built-in tuning. An extra naming an
// option hdiffzTuning already sets replaces it rather than joining it, because
// hdiffz refuses an option given twice -- appending -c-zstd-19-24 aborts the
// diff with "options -c- ERROR!" instead of lowering the level, which would
// make the two settings most worth measuring the two that cannot be passed.
func hdiffzArgs(extra []string) []string {
	replaced := make(map[string]bool, len(extra))
	for _, arg := range extra {
		replaced[hdiffzOption(arg)] = true
	}
	args := make([]string, 0, len(hdiffzTuning)+len(extra))
	for _, arg := range hdiffzTuning {
		if !replaced[hdiffzOption(arg)] {
			args = append(args, arg)
		}
	}
	return append(args, extra...)
}

// ParseHdiffzArgs splits a command line's worth of extra hdiffz options into
// arguments. Every field has to be an option, because hdiffz reads its old, new
// and diff paths positionally: a bare word would take the place of one of them,
// and hdiffz would either diff the wrong file or write the diff over it.
// Refusing here means a typo costs nothing, rather than surfacing as a failed
// diff partway through a generate.
func ParseHdiffzArgs(s string) ([]string, error) {
	args := strings.Fields(s)
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") || hdiffzOption(arg) == "-" {
			return nil, fmt.Errorf("extra hdiffz options are each a dash and an option name: %q is not one", arg)
		}
	}
	return args, nil
}

// HdiffzTuning is the built-in diff tuning, for a caller that wants to show what
// its extra options are being merged into. The copy keeps a caller from editing
// the tuning itself.
func HdiffzTuning() []string {
	return append([]string{}, hdiffzTuning...)
}

// runHdiffz diffs old against updated and returns the patch. extra holds any
// options a caller is measuring with, merged over the built-in tuning.
func runHdiffz(ctx context.Context, old, updated []byte, extra []string) ([]byte, error) {
	oldFile, err := newMemFileWith("old", old)
	if err != nil {
		return nil, err
	}
	defer oldFile.Close()
	newFile, err := newMemFileWith("new", updated)
	if err != nil {
		return nil, err
	}
	defer newFile.Close()
	diffFile, err := newMemFile("diff")
	if err != nil {
		return nil, err
	}
	defer diffFile.Close()

	args := append(hdiffzArgs(extra), "-f",
		oldFile.Path(), newFile.Path(), diffFile.Path())
	cmd, err := toolCommand(ctx, "hdiffz", args...)
	if err != nil {
		return nil, err
	}
	// hdiffz reports what it did on stdout, at length, and nothing reads it;
	// what a failure has to say it says on stderr.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hdiffz failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if _, err := diffFile.File().Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(diffFile.File())
}

// runHpatchzFiles applies patchFile to oldFile, leaving exactly wantLen bytes in
// outFile.
//
// The length is checked here rather than by the caller because it is the only
// thing that can be checked before the bytes are used: a patch that reconstructs
// the wrong length has already gone wrong, whatever the blocks then recompress
// to.
func runHpatchzFiles(ctx context.Context, oldFile, patchFile, outFile *memFile, wantLen int64) error {
	args := append(append([]string{}, hpatchzTuning...), "-f",
		oldFile.Path(), patchFile.Path(), outFile.Path())
	cmd, err := toolCommand(ctx, "hpatchz", args...)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hpatchz failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	fi, err := outFile.File().Stat()
	if err != nil {
		return err
	}
	if fi.Size() != wantLen {
		return fmt.Errorf("the patch produced %d bytes, the delta says the run is %d", fi.Size(), wantLen)
	}
	return nil
}

// runHpatchz is runHpatchzFiles for callers that have, and want, plain byte
// slices: the metadata blob, which is under a megabyte, and the generator.
func runHpatchz(ctx context.Context, old, patch []byte, wantLen int64) ([]byte, error) {
	oldFile, err := newMemFileWith("old", old)
	if err != nil {
		return nil, err
	}
	defer oldFile.Close()
	patchFile, err := newMemFileWith("patch", patch)
	if err != nil {
		return nil, err
	}
	defer patchFile.Close()
	outFile, err := newMemFile("new")
	if err != nil {
		return nil, err
	}
	defer outFile.Close()

	if err := runHpatchzFiles(ctx, oldFile, patchFile, outFile, wantLen); err != nil {
		return nil, err
	}
	if _, err := outFile.File().Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	out := make([]byte, wantLen)
	if _, err := io.ReadFull(outFile.File(), out); err != nil {
		return nil, err
	}
	return out, nil
}
