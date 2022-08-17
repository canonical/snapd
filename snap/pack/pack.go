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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/integrity"
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
	yaml, err := os.ReadFile(filepath.Join(sourceDir, "meta", "snap.yaml"))
	if err != nil {
		return err
	}
	info, err := loadAndValidate(sourceDir, yaml)
	if err == nil {
		snap.SanitizePlugsSlots(info)
		if len(info.BadInterfaces) > 0 {
			fmt.Fprintln(w, snap.BadInterfacesSummary(info))
		}
	}
	return err
}

func loadAndValidate(sourceDir string, yaml []byte) (*snap.Info, error) {
	container := snapdir.New(sourceDir)

	info, err := snap.InfoFromSnapYaml(yaml)
	if err != nil {
		return nil, err
	}
	snap.AddImplicitHooksFromContainer(info, container)

	if err := snap.Validate(info); err != nil {
		return nil, fmt.Errorf("cannot validate snap %q: %v", info.InstanceName(), err)
	}

	if err := snap.ValidateSnapContainer(container, info, logger.Noticef); err != nil {
		return nil, err
	}
	if _, err := snap.ReadSnapshotYamlFromSnapFile(container); err != nil {
		return nil, err
	}

	if info.SnapType == snap.TypeGadget {
		// TODO:UC20: optionally pass model
		// TODO:UC20: pass validation constraints which indicate intent
		//            to have data encrypted
		ginfo, err := gadget.ReadInfoAndValidate(sourceDir, nil, nil)
		if err != nil {
			return nil, err
		}
		if err := gadget.ValidateContent(ginfo, sourceDir, ""); err != nil {
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

func excludesFile() (filename string, err error) {
	tmpf, err := os.CreateTemp("", ".snap-pack-exclude-")
	if err != nil {
		return "", err
	}

	// inspited by os.WriteFile
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
	// Integrity requests appending integrity data to the snap when set
	Integrity bool
}

var Defaults *Options = nil

// Pack the given sourceDirectory and return the generated
// snap or component file.
func Pack(sourceDir string, opts *Options) (string, error) {
	if opts == nil {
		opts = &Options{}
	}
	switch opts.Compression {
	case "xz", "lzo", "zstd", "":
		// fine
	default:
		return "", fmt.Errorf("cannot use compression %q", opts.Compression)
	}

	// ensure we have valid content
	packFunc := packSnap
	yaml, err := os.ReadFile(filepath.Join(sourceDir, "meta", "snap.yaml"))
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// Maybe a component?
		var errComp error
		if yaml, errComp = os.ReadFile(filepath.Join(sourceDir, "meta", "component.yaml")); errComp != nil {
			return "", err
		}
		packFunc = packComponent
	}

	if opts.TargetDir != "" {
		if err := os.MkdirAll(opts.TargetDir, 0755); err != nil {
			return "", err
		}
	}

	return packFunc(sourceDir, yaml, opts)
}

func packSnap(sourceDir string, yaml []byte, opts *Options) (string, error) {
	info, err := loadAndValidate(sourceDir, yaml)
	if err != nil {
		return "", err
	}

	snapName := snapPath(info, opts.TargetDir, opts.SnapName)
	if err := mksquashfs(sourceDir, snapName, string(info.Type()), opts); err != nil {
		return "", err
	}

	return snapName, nil
}

func packComponent(sourceDir string, yaml []byte, opts *Options) (string, error) {
	cont := snapdir.New(sourceDir)
	ci, err := snap.ReadComponentInfoFromContainer(cont)
	if err != nil {
		return "", err
	}
	compPath := componentPath(ci, opts.TargetDir, opts.SnapName)
	if err := snap.ValidateComponentContainer(cont, compPath, logger.Noticef); err != nil {
		return "", err
	}

	if err := mksquashfs(sourceDir, compPath, "", opts); err != nil {
		return "", err
	}

	return compPath, nil
}

func mksquashfs(sourceDir, fName, snapType string, opts *Options) error {
	excludes, err := excludesFile()
	if err != nil {
		return err
	}
	defer os.Remove(excludes)

	d := squashfs.New(fName)
	if err := d.Build(sourceDir, &squashfs.BuildOpts{
		SnapType:     snapType,
		Compression:  opts.Compression,
		ExcludeFiles: []string{excludes},
	}); err != nil {
		return err
	}

	if opts.Integrity {
		err := integrity.GenerateAndAppend(fName)
		if err != nil {
			return err
		}
	}

	return nil
}

func componentPath(ci *snap.ComponentInfo, targetDir, compName string) string {
	if compName == "" {
		// TODO should we consider architecture as with snaps?
		compName = fmt.Sprintf("%s_%s.comp", ci.FullName(), ci.Version)
	}
	if targetDir != "" && !filepath.IsAbs(compName) {
		compName = filepath.Join(targetDir, compName)
	}
	return compName
}
