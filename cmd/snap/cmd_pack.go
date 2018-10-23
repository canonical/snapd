// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/pack"
)

type packCmd struct {
	CheckSkeleton bool   `long:"check-skeleton"`
	Filename      string `long:"filename"`
	Positional    struct {
		SnapDir   string `positional-arg-name:"<snap-dir>"`
		TargetDir string `positional-arg-name:"<target-dir>"`
	} `positional-args:"yes"`
}

var shortPackHelp = i18n.G("Pack the given directory as a snap")
var longPackHelp = i18n.G(`
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
messages.
`)

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
			"filename": i18n.G("Use this filename"),
		}, nil)
	cmd.extra = func(cmd *flags.Command) {
		// TRANSLATORS: this describes the default filename for a snap, e.g. core_16-2.35.2_amd64.snap
		cmd.FindOptionByLongName("filename").DefaultMask = i18n.G("<name>_<version>_<architecture>.snap")
	}
}

func (x *packCmd) Execute([]string) error {
	if x.Positional.SnapDir == "" {
		x.Positional.SnapDir = "."
	}
	if x.Positional.TargetDir == "" {
		x.Positional.TargetDir = "."
	}

	if x.CheckSkeleton {
		err := pack.CheckSkeleton(x.Positional.SnapDir)
		if err == snap.ErrMissingPaths {
			return nil
		}
		return err
	}

	snapPath, err := pack.Snap(x.Positional.SnapDir, x.Positional.TargetDir, x.Filename)
	if err != nil {
		return fmt.Errorf("cannot pack %q: %v", x.Positional.SnapDir, err)

	}
	fmt.Fprintf(Stdout, "built: %s\n", snapPath)
	return nil
}
