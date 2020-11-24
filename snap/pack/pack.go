// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

package pack

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
)

// this could be shipped as a file like "info", and save on the memory and the
// overhead of creating and removing the tempfile, but on darwin we can't AFAIK
// easily know where it's been placed. As it's not really that big, just
// shipping it in memory seems fine for now.
// from click's click.build.ClickBuilderBase, and there from
// @Dpkg::Source::Package::tar_ignore_default_pattern;
// changed to match squashfs's "-wildcards" syntax
//
// for anchored vs non-anchored syntax see RELEASE_README in squashfs-tools.
const excludesContent = `
# "anchored", only match at top level
DEBIAN
.arch-ids
.arch-inventory
.bzr
.bzr-builddeb
.bzr.backup
.bzr.tags
.bzrignore
.cvsignore
.git
.gitattributes
.gitignore
.gitmodules
.hg
.hgignore
.hgsigs
.hgtags
.shelf
.svn
CVS
DEADJOE
RCS
_MTN
_darcs
{arch}
.snapignore

# "non-anchored", match anywhere
... .[#~]*
... *.snap
... *.click
... .*.sw?
... *~
... ,,*
`

// small helper that returns the architecture of the snap, or "multi" if it's multiple arches
func debArchitecture(info *snap.Info) string {
	switch len(info.Architectures) {
	case 0:
		return "all"
	case 1:
		return info.Architectures[0]
	default:
		return "multi"
	}
}

// CheckSkeleton attempts to validate snap data in source directory
func CheckSkeleton(w io.Writer, sourceDir string) error {
	info, err := loadAndValidate(sourceDir)
	if err == nil {
		snap.SanitizePlugsSlots(info)
		if len(info.BadInterfaces) > 0 {
			fmt.Fprintln(w, snap.BadInterfacesSummary(info))
		}
	}
	return err
}

func loadAndValidate(sourceDir string) (*snap.Info, error) {
	// ensure we have valid content
	yaml, err := ioutil.ReadFile(filepath.Join(sourceDir, "meta", "snap.yaml"))
	if err != nil {
		return nil, err
	}

	info, err := snap.InfoFromSnapYaml(yaml)
	if err != nil {
		return nil, err
	}

	if err := snap.Validate(info); err != nil {
		return nil, fmt.Errorf("cannot validate snap %q: %v", info.InstanceName(), err)
	}

	if err := snap.ValidateContainer(snapdir.New(sourceDir), info, logger.Noticef); err != nil {
		return nil, err
	}

	if info.SnapType == snap.TypeGadget {
		// TODO:UC20: optionally pass model
		// TODO:UC20: pass validation constraints which indicate intent
		//            to have data encrypted
		if err := gadget.Validate(sourceDir, nil, nil); err != nil {
			return nil, err
		}
	}
	if info.SnapType == snap.TypeKernel {
		if err := kernel.Validate(sourceDir); err != nil {
			return nil, err
		}
	}

	return info, nil
}

func snapPath(info *snap.Info, targetDir, snapName string) string {
	if snapName == "" {
		snapName = fmt.Sprintf("%s_%s_%v.snap", info.InstanceName(), info.Version, debArchitecture(info))
	}
	if targetDir != "" && !filepath.IsAbs(snapName) {
		snapName = filepath.Join(targetDir, snapName)
	}
	return snapName
}

func prepare(sourceDir, targetDir string) (*snap.Info, error) {
	info, err := loadAndValidate(sourceDir)
	if err != nil {
		return nil, err
	}

	if targetDir != "" {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, err
		}
	}

	return info, nil
}

func excludesFile() (filename string, err error) {
	tmpf, err := ioutil.TempFile("", ".snap-pack-exclude-")
	if err != nil {
		return "", err
	}

	// inspited by ioutil.WriteFile
	n, err := tmpf.Write([]byte(excludesContent))
	if err == nil && n < len(excludesContent) {
		err = io.ErrShortWrite
	}

	if err1 := tmpf.Close(); err == nil {
		err = err1
	}

	if err == nil {
		filename = tmpf.Name()
	}

	return filename, err
}

type Options struct {
	// TargetDir is the direction where the snap file will be placed, or empty
	// to use the current directory
	TargetDir string
	// SnapName is the name of the snap file, or empty to use the default name
	// which is <snapname>_<version>_<architecture>.snap
	SnapName string
	// Compression method to use
	Compression string
}

var Defaults *Options = nil

// Snap the given sourceDirectory and return the generated
// snap file
func Snap(sourceDir string, opts *Options) (string, error) {
	if opts == nil {
		opts = &Options{}
	}
	switch opts.Compression {
	case "xz", "lzo", "":
		// fine
	default:
		return "", fmt.Errorf("cannot use compression %q", opts.Compression)
	}

	info, err := prepare(sourceDir, opts.TargetDir)
	if err != nil {
		return "", err
	}

	excludes, err := excludesFile()
	if err != nil {
		return "", err
	}
	defer os.Remove(excludes)

	snapName := snapPath(info, opts.TargetDir, opts.SnapName)
	d := squashfs.New(snapName)
	if err = d.Build(sourceDir, &squashfs.BuildOpts{
		SnapType:     string(info.Type()),
		Compression:  opts.Compression,
		ExcludeFiles: []string{excludes},
	}); err != nil {
		return "", err
	}

	return snapName, nil
}
