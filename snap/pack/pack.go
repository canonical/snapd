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
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
)

// from click's click.build.ClickBuilderBase, and there from
// @Dpkg::Source::Package::tar_ignore_default_pattern;
// changed to regexps from globs for sanity (hah)
//
// Please resist the temptation of optimizing the regexp by grouping
// things by hand. People will find it unreadable enough as it is.
var shouldExcludeDefault = regexp.MustCompile(strings.Join([]string{
	`\.snap$`, // added
	`\.click$`,
	`^\..*\.sw.$`,
	`~$`,
	`^,,`,
	`^\.[#~]`,
	`^\.arch-ids$`,
	`^\.arch-inventory$`,
	`^\.bzr$`,
	`^\.bzr-builddeb$`,
	`^\.bzr\.backup$`,
	`^\.bzr\.tags$`,
	`^\.bzrignore$`,
	`^\.cvsignore$`,
	`^\.git$`,
	`^\.gitattributes$`,
	`^\.gitignore$`,
	`^\.gitmodules$`,
	`^\.hg$`,
	`^\.hgignore$`,
	`^\.hgsigs$`,
	`^\.hgtags$`,
	`^\.shelf$`,
	`^\.svn$`,
	`^CVS$`,
	`^DEADJOE$`,
	`^RCS$`,
	`^_MTN$`,
	`^_darcs$`,
	`^{arch}$`,
	`^\.snapignore$`,
}, "|")).MatchString

// fake static function variables
type keep struct {
	basedir string
	exclude func(string) bool
}

func (k *keep) shouldExclude(basedir string, file string) bool {
	if basedir == k.basedir {
		if k.exclude == nil {
			return false
		}

		return k.exclude(file)
	}

	k.basedir = basedir
	k.exclude = nil

	snapignore, err := os.Open(filepath.Join(basedir, ".snapignore"))
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(snapignore)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := regexp.Compile(line); err != nil {
			// not a regexp
			line = regexp.QuoteMeta(line)
		}
		lines = append(lines, line)
	}

	fullRegex := strings.Join(lines, "|")
	exclude, err := regexp.Compile(fullRegex)
	if err == nil {
		k.exclude = exclude.MatchString

		return k.exclude(file)
	}

	// can't happen; can't even find a way to trigger it in testing.
	panic(fmt.Sprintf("internal error: composition of valid regexps is invalid?!? Please report this bug: %#v", fullRegex))
}

var shouldExcludeDynamic = new(keep).shouldExclude

func shouldExclude(basedir string, file string) bool {
	return shouldExcludeDefault(file) || shouldExcludeDynamic(basedir, file)
}

// small helper that return the architecture or "multi" if its multiple arches
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

func copyToBuildDir(sourceDir, buildDir string) error {
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}

	err = os.Remove(buildDir)
	if err != nil && !os.IsNotExist(err) {
		// this shouldn't happen, but.
		return err
	}

	// no umask here so that we get the permissions correct
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, errin error) (err error) {
		if errin != nil {
			return errin
		}

		relpath := path[len(sourceDir):]
		if relpath == "/DEBIAN" || shouldExclude(sourceDir, filepath.Base(path)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dest := filepath.Join(buildDir, relpath)

		// handle dirs
		if info.IsDir() {
			if err := os.Mkdir(dest, info.Mode()); err != nil {
				return err
			}
			// ensure that permissions are preserved
			uid := sys.UserID(info.Sys().(*syscall.Stat_t).Uid)
			gid := sys.GroupID(info.Sys().(*syscall.Stat_t).Gid)
			return sys.ChownPath(dest, uid, gid)
		}

		// handle char/block devices
		if osutil.IsDevice(info.Mode()) {
			return osutil.CopySpecialFile(path, dest)
		}

		if (info.Mode() & os.ModeSymlink) != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dest)
		}

		// fail if its unsupported
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot handle type of file %s", path)
		}

		// it's a file. Maybe we can link it?
		if os.Link(path, dest) == nil {
			// whee
			return nil
		}
		// sigh. ok, copy it is.
		return osutil.CopyFile(path, dest, osutil.CopyFlagDefault)
	})
}

// CheckSkeleton attempts to validate snap data in source directory
func CheckSkeleton(sourceDir string) error {
	_, err := loadAndValidate(sourceDir)
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
		return nil, err
	}

	if err := snap.ValidateContainer(snapdir.New(sourceDir), info, logger.Noticef); err != nil {
		return nil, err
	}
	return info, nil
}

func prepare(sourceDir, targetDir, buildDir string) (snapName string, err error) {
	info, err := loadAndValidate(sourceDir)
	if err != nil {
		return "", err
	}

	if err := copyToBuildDir(sourceDir, buildDir); err != nil {
		return "", err
	}

	// build the package
	snapName = fmt.Sprintf("%s_%s_%v.snap", info.InstanceName(), info.Version, debArchitecture(info))

	if targetDir != "" {
		snapName = filepath.Join(targetDir, snapName)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return "", err
			}
		}
	}

	return snapName, nil
}

// Snap the given sourceDirectory and return the generated
// snap file
func Snap(sourceDir, targetDir string) (string, error) {
	// create build dir
	buildDir, err := ioutil.TempDir("", "snappy-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(buildDir)

	snapName, err := prepare(sourceDir, targetDir, buildDir)
	if err != nil {
		return "", err
	}

	d := squashfs.New(snapName)
	if err = d.Build(buildDir); err != nil {
		return "", err
	}

	return snapName, nil
}
