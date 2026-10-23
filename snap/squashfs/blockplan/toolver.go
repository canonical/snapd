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
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// SEC_TOOLVER records the versions of the tools a delta was built with: the
// hdiffz/hpatchz pair its patches were made with, and the compressor that
// reproduced the image's blocks.
//
// The section is advisory on purpose. Byte-exactness is already proven twice --
// at generation, and again by the canary, the per-block checks and the final
// digest at apply -- so a version mismatch cannot corrupt an apply; it only
// explains one. When a delta built with drifted tools fails a byte check, the
// warning says which tool to align instead of leaving the operator to bisect.
//
// An applier that predates the section reads and ignores it like any unknown
// section, and a delta that predates it simply carries none, so the section
// needs no format or tools version bump.

// toolVersionLine runs tool with args and returns "tool: " plus the first line
// of its output. Any failure -- missing tool, non-zero exit, empty output --
// yields "": a version that cannot be probed must not break a generation or an
// apply.
func toolVersionLine(ctx context.Context, tool string, args ...string) string {
	cmd, err := toolCommand(ctx, tool, args...)
	if err != nil {
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if line == "" {
		return ""
	}
	return tool + ": " + line
}

// captureToolVersions builds SEC_TOOLVER for a delta this machine is about to
// ship. For lzo and zstd the version comes from the very library the blocks were
// compressed with, which both constructors have dlopened by the time a
// Compressor exists at all. A probe that fails contributes no line, and when
// nothing can be probed the section is omitted.
func captureToolVersions(ctx context.Context, comp Compressor) []byte {
	var lines []string
	if v := toolVersionLine(ctx, "hdiffz", "-v"); v != "" {
		lines = append(lines, v)
	}
	if v := toolVersionLine(ctx, "hpatchz", "-v"); v != "" {
		lines = append(lines, v)
	}
	if v := comp.ToolVersion(); v != "" {
		lines = append(lines, v)
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// checkToolVersions compares the delta's recorded versions against the tools
// this apply actually invokes -- hpatchz and the image's compressor -- and
// reports drift to w. It never fails the apply: the canary and the byte checks
// are the gates, and the warning exists to turn a "why did the bytes not match"
// into "these versions drifted, here they are". Which is also why it takes a
// writer rather than reaching for the log: a caller that wants to see the
// warnings, a test included, can hold on to them.
func checkToolVersions(ctx context.Context, w io.Writer, recorded []byte, comp Compressor) {
	want := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(recorded), "\n"), "\n") {
		if name, ver, ok := strings.Cut(line, ": "); ok && name != "" {
			want[name] = ver
		}
	}
	got := map[string]string{}
	// hdiffz is not probed: it does not run on the applying machine, so its
	// recorded line has nothing to be compared against.
	if v := toolVersionLine(ctx, "hpatchz", "-v"); v != "" {
		if name, ver, ok := strings.Cut(v, ": "); ok {
			got[name] = ver
		}
	}
	if v := comp.ToolVersion(); v != "" {
		if name, ver, ok := strings.Cut(v, ": "); ok {
			got[name] = ver
		}
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cur, ok := got[name]; ok && cur != want[name] {
			fmt.Fprintf(w, "warning: %s drifted since the delta was built: it used %q, this machine has %q\n",
				name, want[name], cur)
		}
	}
}
