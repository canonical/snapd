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
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// This file turns the two statistics structs into something a person reads.
//
// It lives in this package rather than in the command that prints it because
// the numbers and their units are defined here: humanBytes is here, and so is
// the commentary saying which field means what. A report written a package away
// would have to re-derive both, and would drift from the fields the moment one
// was added.

// WriteGenerateReport describes a finished delta: what it is made of, and --
// the point of the format -- how much compression applying it avoids.
func WriteGenerateReport(w io.Writer, s *Stats, sourceSnap, targetSnap string, verified bool) {
	fmt.Fprintf(w, "%s -> %s\n", sourceSnap, targetSnap)
	fmt.Fprintf(w, "  delta          %s in %.1fs\n", humanBytes(s.DeltaSize), s.Elapsed.Seconds())
	fmt.Fprintf(w, "  instructions   %d (%d copy, %d literal, %d patch run), %s encoded -> %s stored\n",
		s.Instructions, s.Copies, s.Literals, s.PatchRuns,
		humanBytes(int64(s.InstrBytes)), humanBytes(int64(s.InstrStored)))
	fmt.Fprintf(w, "  data region    %s on disk, %s of plaintext\n",
		humanBytes(s.TargetDataBytes), humanBytes(s.TargetUBytes))
	fmt.Fprintf(w, "  copied         %s (%.1f%% of on-disk bytes)\n",
		humanBytes(s.CopiedBytes), pct(s.CopiedBytes, s.TargetDataBytes))
	fmt.Fprintf(w, "  literal        %s\n", humanBytes(s.LiteralBytes))
	fmt.Fprintf(w, "  patch runs     %s of patch rebuilding %s of plaintext\n",
		humanBytes(s.PatchBytes), humanBytes(s.PatchedUBytes))
	if n := s.RunsNoWindow + s.RunsTooExpensive + s.RunsVerifyFailed; n > 0 {
		fmt.Fprintf(w, "  runs as literal %d (%d no window, %d not worth it, %d failed verify), %s\n",
			n, s.RunsNoWindow, s.RunsTooExpensive, s.RunsVerifyFailed,
			humanBytes(s.RunsRejectedBytes))
	}
	if anchored := s.RunsPathAnchored + s.RunsFuzzyAnchored + s.RunsCursorAnchored; anchored > 0 {
		fmt.Fprintf(w, "  anchored by    %d path, %d version-bump path, %d source offset\n",
			s.RunsPathAnchored, s.RunsFuzzyAnchored, s.RunsCursorAnchored)
	}
	if s.MatchUnavailable != "" {
		fmt.Fprintf(w, "  no path map    %s\n", s.MatchUnavailable)
	}
	fmt.Fprintf(w, "  metadata       %d blocks, %s of plaintext, %s patch\n",
		s.MetaBlocks, humanBytes(s.MetaUBytes), humanBytes(int64(s.MDPatchBytes)))
	// The headline. The pseudo-file format decompresses the whole source and
	// recompresses the whole target, so the comparison is against the target's
	// entire plaintext; here the compressor only ever sees a patch run's blocks
	// plus the metadata region.
	whole := s.TargetUBytes + s.MetaUBytes
	comp := s.PatchedUBytes + s.MetaUBytes
	fmt.Fprintf(w, "  apply compresses %s of %s (%.1f%% avoided) and decompresses %s\n",
		humanBytes(comp), humanBytes(whole), pct(whole-comp, whole),
		humanBytes(s.WindowUBytes))
	if verified {
		fmt.Fprintf(w, "  verified       the delta reconstructs %s exactly\n", targetSnap)
	}
}

// WriteApplyReport describes what an apply cost, which is the number the format
// exists to reduce.
func WriteApplyReport(w io.Writer, s *ApplyStats, elapsed time.Duration) {
	fmt.Fprintf(w, "applied in %.1fs\n", elapsed.Seconds())
	fmt.Fprintf(w, "  instructions   %d (%d copy, %d literal, %d patch run)\n",
		s.Instructions, s.Copies, s.Literals, s.PatchRuns)
	fmt.Fprintf(w, "  copied         %s straight from the source, no compressor\n", humanBytes(s.CopiedBytes))
	fmt.Fprintf(w, "  literal        %s from the delta\n", humanBytes(s.LiteralBytes))
	fmt.Fprintf(w, "  patched        %s of plaintext rebuilt from %s of source plaintext\n",
		humanBytes(s.PatchedBytes), humanBytes(s.WindowUBytes))
	fmt.Fprintf(w, "  compressed     %s of plaintext in %d blocks\n",
		humanBytes(s.UCompressedBytes), s.BlocksCompressed)
	fmt.Fprintf(w, "                 of which metadata %s in %d blocks\n",
		humanBytes(s.MetaUBytes), s.MetaBlocks)
	if rss := peakRSS(); rss > 0 {
		fmt.Fprintf(w, "  peak RSS       %s in this process\n", humanBytes(rss))
	}
	fmt.Fprintf(w, "  peak scratch   %s in memfds (RAM, but not resident)\n",
		humanBytes(s.PeakScratchBytes))
}

// peakRSS is this process's own high-water resident size, which is the quantity
// the job count and the run cap are between them supposed to bound. It is worth
// reporting separately from `/usr/bin/time -v`, which reports the maximum over
// the whole process tree: a heavy xz or hpatchz child there hides -- or takes
// the blame for -- what the applier itself holds. Returns 0 if the kernel does
// not offer VmHWM.
func peakRSS() int64 {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "VmHWM:"))
		if len(fields) < 1 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return kb << 10
	}
	return 0
}

func pct(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}
