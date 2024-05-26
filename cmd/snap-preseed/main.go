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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/image/preseed"

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
	Reset               bool   `long:"reset"`
	ResetChroot         bool   `long:"reset-chroot" hidden:"1"`
	PreseedSignKey      string `long:"preseed-sign-key"`
	AppArmorFeaturesDir string `long:"apparmor-features-dir"`
	SysfsOverlay        string `long:"sysfs-overlay"`
}

var (
	osGetuid = os.Getuid
	// unused currently, left in place for consistency for when it is needed
	// Stdout   io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr

	preseedCore20               = preseed.Core20
	preseedClassic              = preseed.Classic
	preseedClassicReset         = preseed.ClassicReset
	preseedResetPreseededChroot = preseed.ResetPreseededChroot

	opts options
)

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
	mylog.Check(run(parser, os.Args[1:]))
}

func run(parser *flags.Parser, args []string) (err error) {
	// real validation of plugs and slots; needs to be set
	// for processing of seeds with gadget because of readInfo().
	snap.SanitizePlugsSlots = builtin.SanitizePlugsSlots

	rest := mylog.Check2(parser.ParseArgs(args))

	if osGetuid() != 0 {
		return fmt.Errorf("must be run as root")
	}

	var chrootDir string
	if opts.ResetChroot {
		chrootDir = "/"
	} else {
		if len(rest) == 0 {
			return fmt.Errorf("need chroot path as argument")
		}

		chrootDir = mylog.Check2(filepath.Abs(rest[0]))

		// safety check
		if chrootDir == "/" {
			return fmt.Errorf("cannot run snap-preseed against /")
		}
	}

	if probeCore20ImageDir(chrootDir) {
		if opts.Reset || opts.ResetChroot {
			return fmt.Errorf("cannot snap-preseed --reset for Ubuntu Core")
		}

		coreOpts := &preseed.CoreOptions{
			PrepareImageDir:           chrootDir,
			PreseedSignKey:            opts.PreseedSignKey,
			AppArmorKernelFeaturesDir: opts.AppArmorFeaturesDir,
			SysfsOverlay:              opts.SysfsOverlay,
		}
		return preseedCore20(coreOpts)
	}
	if opts.ResetChroot {
		return preseedResetPreseededChroot(chrootDir)
	}
	if opts.Reset {
		return preseedClassicReset(chrootDir)
	}
	return preseedClassic(chrootDir)
}
