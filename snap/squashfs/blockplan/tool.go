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
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

// This file resolves the external tools this package drives: xz to reproduce
// blocks, hdiffz to make a patch and hpatchz to apply one.
//
// The copy shipped in the snapd (or core) snap comes first, so that a device
// applying a delta uses the tools snapd was tested with rather than whatever
// the base happens to carry, and $PATH is the fallback -- which is what makes
// the package usable from a build machine, from the spread tests and from a
// classic system that has the tools installed.
//
// Not finding a tool is not an error worth reporting upwards as a failure: it
// is a refusal. Generate and apply both check what they need before they start,
// and a caller that cannot make or use this format falls back to the format
// below it.

var snapdtoolCommandFromSystemSnapWithContext = snapdtool.CommandFromSystemSnapWithContext

// toolCommand builds a command that runs one of the external tools, from the
// snapd or core snap where that snap carries it and from $PATH otherwise.
//
// Every invocation goes through here rather than through a resolved path,
// because the snap copy is not always executable by path alone: a snapd snap
// mounted somewhere other than /snap has to be invoked through its own ELF
// interpreter, with a library path to match, and snapdtool builds that command.
func toolCommand(ctx context.Context, tool string, args ...string) (*exec.Cmd, error) {
	if inSnap := systemSnapTool(tool); inSnap != "" {
		cmd, err := snapdtoolCommandFromSystemSnapWithContext(ctx, inSnap, args...)
		if err == nil {
			return cmd, nil
		}
		// Fall through to $PATH: the tool is there but something about
		// invoking it from the snap could not be worked out, and a tool
		// that runs is better than a refusal.
	}
	path, err := exec.LookPath(tool)
	if err != nil {
		return nil, fmt.Errorf("cannot find %s: %w", tool, err)
	}
	return exec.CommandContext(ctx, path, args...), nil
}

// systemSnapTool returns the snapd-snap-relative path of tool if the system
// snap actually carries it, and "" if it does not.
//
// The existence check is the point of this function. snapdtool builds a command
// from a path it never looks at, so asking it for a tool the snap does not have
// yields a command that fails at exec time -- which for a tool this package
// only optionally needs would turn a clean fallback to $PATH into a failed
// generate. The snapd snap has shipped xdelta3 for a while and xz and hpatchz
// only recently, so both cases are live.
func systemSnapTool(tool string) string {
	rel := filepath.Join("/usr/bin", tool)
	// Mirror CommandFromSystemSnap's own choice of snap, so that the file
	// checked here is the file it will build a command for.
	root := filepath.Join(dirs.SnapMountDir, "snapd", "current")
	if !osutil.FileExists(root) {
		root = filepath.Join(dirs.SnapMountDir, "core", "current")
	}
	if !osutil.FileExists(filepath.Join(root, rel)) {
		return ""
	}
	return rel
}

// haveTool reports whether tool can be run on this system at all.
//
// It is what generate and apply gate on up front: a missing hdiffz means no
// delta can be made here, and a missing hpatchz or xz means one cannot be
// applied, and in both cases saying so before any image work starts is what
// lets the caller fall back cheaply.
func haveTool(tool string) bool {
	_, err := toolCommand(context.Background(), tool)
	return err == nil
}
