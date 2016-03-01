// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// Package policy provides helpers for keeping a framework's security policies
// up to date on install/remove.
package policy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/osutil"
)

var (
	// SecBase is the directory to which the security policies and templates
	// are copied
	SecBase = "/var/lib/snappy"
)

type policyOp uint

const (
	install policyOp = iota
	remove
)

func (op policyOp) String() string {
	switch op {
	case remove:
		return "Remove"
	case install:
		return "Install"
	default:
		return fmt.Sprintf("policyOp(%d)", op)
	}
}

// iterOp iterates over all the files found with the given glob, making the
// basename (with the given prefix prepended) the target file in the given
// target directory. It then performs op on that target file: either copying
// from the globbed file to the target file, or removing the target file.
// Directories are created as needed. Errors out with any of the things that
// could go wrong with this, including a file found by glob not being a
// regular file.
func iterOp(op policyOp, glob, targetDir, prefix string) (err error) {
	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("unable to make %v directory: %v", targetDir, err)
	}

	files, err := filepath.Glob(glob)
	if err != nil {
		// filepath.Glob seems to not return errors ever right
		// now. This might be a bug in Go, or it might be by
		// design. Better play safe.
		return fmt.Errorf("unable to glob %v: %v", glob, err)
	}

	for _, file := range files {
		s, err := os.Lstat(file)
		if err != nil {
			return fmt.Errorf("unable to stat %v: %v", file, err)
		}

		if !s.Mode().IsRegular() {
			return fmt.Errorf("unable to do %s for %v: not a regular file", op, file)
		}

		targetFile := filepath.Join(targetDir, prefix+filepath.Base(file))
		switch op {
		case remove:
			if err := os.Remove(targetFile); err != nil {
				return fmt.Errorf("unable to remove %v: %v", targetFile, err)
			}
		case install:
			// do the copy
			if err := osutil.CopyFile(file, targetFile, osutil.CopyFlagSync|osutil.CopyFlagOverwrite); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown operation %s", op)
		}
	}

	return nil
}

// frameworkOp perform the given operation (either Install or Remove) on the
// given package that's installed in the given path.
func frameworkOp(op policyOp, pkgName, instPath, rootDir string) error {
	pol := filepath.Join(instPath, "meta", "framework-policy")
	for _, i := range []string{"apparmor", "seccomp"} {
		for _, j := range []string{"policygroups", "templates"} {
			if err := iterOp(op, filepath.Join(pol, i, j, "*"), filepath.Join(rootDir, SecBase, i, j), pkgName+"_"); err != nil {
				return err
			}
		}
	}

	return nil
}

// Install sets up the framework's policy from the given snap that's
// installed in the given path.
func Install(pkgName, instPath, rootDir string) error {
	return frameworkOp(install, pkgName, instPath, rootDir)
}

// Remove cleans up the framework's policy from the given snap that's
// installed in the given path.
func Remove(pkgName, instPath, rootDir string) error {
	return frameworkOp(remove, pkgName, instPath, rootDir)
}

func aaUp(old, new, dir, pfx string) map[string]bool {
	return osutil.DirUpdated(filepath.Join(old, dir), filepath.Join(new, dir), pfx)
}

// AppArmorDelta returns which policies and templates are updated in the package
// at newPath, as compared to those installed in the system. The given prefix is
// applied to the keys of the returns maps.
func AppArmorDelta(oldPath, newPath, prefix string) (policies map[string]bool, templates map[string]bool) {
	newaa := filepath.Join(newPath, "meta", "framework-policy", "apparmor")
	oldaa := filepath.Join(oldPath, "meta", "framework-policy", "apparmor")

	return aaUp(oldaa, newaa, "policygroups", prefix), aaUp(oldaa, newaa, "templates", prefix)
}
