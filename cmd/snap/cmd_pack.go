// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

package main

import (
	"fmt"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/i18n"

	// for SanitizePlugsSlots
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/pack"
)

type packCmd struct {
	CheckSkeleton bool   `long:"check-skeleton"`
	AppendVerity  bool   `long:"append-integrity-data" hidden:"yes"`
	Filename      string `long:"filename"`
	Compression   string `long:"compression"`
	Positional    struct {
		SnapDir   string `positional-arg-name:"<snap-dir>"`
		TargetDir string `positional-arg-name:"<target-dir>"`
	} `positional-args:"yes"`
}

var (
	shortPackHelp = i18n.G("Pack the given directory as a snap")
	longPackHelp  = i18n.G(`
The pack command packs the given snap-dir as a snap and writes the result to
target-dir. If target-dir is omitted, the result is written to current
directory. If both source-dir and target-dir are omitted, the pack command packs
the current directory.

The default file name for a snap can be derived entirely from its snap.yaml, but
in some situations it's simpler for a script to feed the filename in. In those
cases, --filename can be given to override the default. If this filename is
not absolute it will be taken as relative to target-dir.

When used with --check-skeleton, pack only checks whether snap-dir contains
valid snap metadata and raises an error otherwise. Application commands listed
in snap metadata file, but appearing with incorrect permission bits result in an
error. Commands that are missing from snap-dir are listed in diagnostic
messages.`,

	/*
	   When used with --append-integrity-data, pack will append dm-verity data at the end
	   of the snap to be used with snapd's snap integrity verification mechanism.
	*/
	)
)

func init() {
	cmd := addCommand("pack",
		shortPackHelp,
		longPackHelp,
		func() flags.Commander {
			return &packCmd{}
		}, map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"check-skeleton": i18n.G("Validate snap-dir metadata only"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"filename": i18n.G("Output to this filename"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"compression": i18n.G("Compression to use (e.g. xz or lzo)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"append-integrity-data": i18n.G("Generate and append dm-verity data"),
		}, nil)
	cmd.extra = func(cmd *flags.Command) {
		// TRANSLATORS: this describes the default filename for a snap, e.g. core_16-2.35.2_amd64.snap
		cmd.FindOptionByLongName("filename").DefaultMask = i18n.G("<name>_<version>_<architecture>.snap")
	}
}

func (x *packCmd) Execute([]string) error {
	// plug/slot sanitization is disabled (no-op) by default at the package level for "snap" command,
	// for "snap pack" however we want real validation.
	snap.SanitizePlugsSlots = builtin.SanitizePlugsSlots

	if x.Positional.TargetDir != "" && x.Filename != "" && filepath.IsAbs(x.Filename) {
		return fmt.Errorf(i18n.G("you can't specify an absolute filename while also specifying target dir."))
	}

	if x.Positional.SnapDir == "" {
		x.Positional.SnapDir = "."
	}
	if x.Positional.TargetDir == "" {
		x.Positional.TargetDir = "."
	}

	if x.CheckSkeleton {
		mylog.Check(pack.CheckSkeleton(Stderr, x.Positional.SnapDir))
		if err == snap.ErrMissingPaths {
			return nil
		}
		return err
	}

	snapPath := mylog.Check2(pack.Pack(x.Positional.SnapDir, &pack.Options{
		TargetDir:   x.Positional.TargetDir,
		SnapName:    x.Filename,
		Compression: x.Compression,
		Integrity:   x.AppendVerity,
	}))

	// TRANSLATORS: the %q is the snap-dir (the first positional
	// argument to the command); the %v is an error

	// TRANSLATORS: %s is the path to the built snap file
	fmt.Fprintf(Stdout, i18n.G("built: %s\n"), snapPath)
	return nil
}
