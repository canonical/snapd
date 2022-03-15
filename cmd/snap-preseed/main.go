// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	// for SanitizePlugsSlots
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

const (
	shortHelp = "Prerun the first boot seeding of snaps in an image filesystem chroot with a snapd seed."
	longHelp  = `
The snap-preseed command takes a directory containing an image, including seed
snaps (at /var/lib/snapd/seed), and runs through the snapd first-boot process
up to hook execution. No boot actions unrelated to snapd are performed.
It creates systemd units for seeded snaps, makes any connections, and generates
security profiles. The image is updated and consequently optimised to reduce
first-boot startup time`
)

type options struct {
	Reset bool `long:"reset"`
}

var (
	osGetuid           = os.Getuid
	Stdout   io.Writer = os.Stdout
	Stderr   io.Writer = os.Stderr

	opts options
)

type PreseedOpts struct {
	PrepareImageDir  string
	PreseedChrootDir string
	SystemLabel      string
	WritableDir      string
}

func Parser() *flags.Parser {
	opts = options{}
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	parser.ShortDescription = shortHelp
	parser.LongDescription = longHelp
	return parser
}

func probeCore20ImageDir(dir string) bool {
	sysDir := filepath.Join(dir, "system-seed")
	_, isDir, _ := osutil.DirExists(sysDir)
	return isDir
}

func main() {
	parser := Parser()
	if err := run(parser, os.Args[1:]); err != nil {
		fmt.Fprintf(Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(parser *flags.Parser, args []string) (err error) {
	// real validation of plugs and slots; needs to be set
	// for processing of seeds with gadget because of readInfo().
	snap.SanitizePlugsSlots = builtin.SanitizePlugsSlots

	if osGetuid() != 0 {
		return fmt.Errorf("must be run as root")
	}

	rest, err := parser.ParseArgs(args)
	if err != nil {
		return err
	}

	if len(rest) == 0 {
		return fmt.Errorf("need chroot path as argument")
	}

	chrootDir, err := filepath.Abs(rest[0])
	if err != nil {
		return err
	}

	// safety check
	if chrootDir == "/" {
		return fmt.Errorf("cannot run snap-preseed against /")
	}

	if opts.Reset {
		return resetPreseededChroot(chrootDir)
	}

	var cleanup func()
	if probeCore20ImageDir(chrootDir) {
		var popts *PreseedOpts
		popts, cleanup, err = prepareCore20Chroot(chrootDir)
		if err != nil {
			return err
		}

		err = runUC20PreseedMode(popts)
	} else {
		if err := checkChroot(chrootDir); err != nil {
			return err
		}

		var targetSnapd *targetSnapdInfo

		// XXX: if prepareClassicChroot & runPreseedMode were refactored to
		// use "chroot" inside runPreseedMode (and not syscall.Chroot at the
		// beginning of prepareClassicChroot), then we could have a single
		// runPreseedMode/runUC20PreseedMode function that handles both classic
		// and core20.
		targetSnapd, cleanup, err = prepareClassicChroot(chrootDir)
		if err != nil {
			return err
		}

		// executing inside the chroot
		err = runPreseedMode(chrootDir, targetSnapd)
	}

	cleanup()
	return err
}
