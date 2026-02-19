// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"context"
	"fmt"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
)

var shortDeltaHelp = i18n.G("Apply/Generate snap delta")
var longDeltaHelp = i18n.G(`
Generate / apply 'smart' delta between source and target snaps

Operations:
  generate : generate delta between source and target
  apply    : apply delta on the source

Formats:
  xdelta3          : raw xdelta3 binary diff
  snap-1-1-xdelta3 : snap-aware xdelta3 delta

Examples:
  $ snap delta generate --source <source snap file> --target <target snap file> --delta <delta file> --format snap-1-1-xdelta3
  $ snap delta generate --source <source snap file> --target <target snap file> --delta <delta file> --format xdelta3
  $ snap delta apply --delta <delta file> --source <source snap file> --target <target snap file>
`)

type cmdDelta struct {
	clientMixin
	Positional struct {
		Operation string `positional-args:"yes" choices:"apply|generate"`
	} `positional-args:"yes" required:"yes"`

	Source string `long:"source" short:"s" required:"yes"`
	Target string `long:"target" short:"t" required:"yes"`
	Delta  string `long:"delta" short:"d" required:"yes"`
	Format string `long:"format" short:"f"`
}

// override for testing
var (
	squashfsGenerateDelta = squashfs.GenerateDelta
	squashfsApplyDelta    = squashfs.ApplyDelta
)

func init() {
	cmd := addCommand("delta", shortDeltaHelp, longDeltaHelp, func() flags.Commander { return &cmdDelta{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"source": i18n.G("Source snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"target": i18n.G("Target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"delta": i18n.G("Delta between source and target snap package"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"format": i18n.G("Delta format algorithm, one of: xdelta3, snap-1-1-xdelta3"),
		}, []argDesc{
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<operation>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The delta operation to perform, one of: apply|generate"),
			},
		})
	cmd.hidden = true
}

func (x *cmdDelta) Execute(args []string) error {
	// Listen for SIGINT/SIGTERM and cancel the context so that
	// subprocesses (xdelta3, unsquashfs, mksquashfs) are stopped.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh, sigStop := signalNotify(syscall.SIGINT, syscall.SIGTERM)
	defer sigStop()
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	switch x.Positional.Operation {
	case "generate":
		if x.Format == "" {
			return fmt.Errorf(i18n.G("the --format flag is required for generate, supported formats: %s"),
				strings.Join(squashfs.SupportedDeltaFormats(), ", "))
		}
		if !strutil.ListContains(squashfs.SupportedDeltaFormats(), x.Format) {
			return fmt.Errorf(i18n.G("unsupported delta format %q, supported formats: %s"),
				x.Format, strings.Join(squashfs.SupportedDeltaFormats(), ", "))
		}
		fmt.Fprintf(Stdout, i18n.G("Using snap delta algorithm '%s'\n"), x.Format)
		fmt.Fprintf(Stdout, i18n.G("Generating delta...\n"))
		return squashfsGenerateDelta(ctx, x.Source, x.Target, x.Delta, x.Format)
	case "apply":
		fmt.Fprintf(Stdout, i18n.G("Applying delta...\n"))
		return squashfsApplyDelta(ctx, x.Source, x.Delta, x.Target)
	}

	return fmt.Errorf(i18n.G("unknown operation: %s"), x.Positional.Operation)
}
