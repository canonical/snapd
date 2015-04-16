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
	"io"
	"os"
	"path/filepath"

	"launchpad.net/snappy/helpers"
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
func iterOp(op policyOp, glob string, targetDir string, prefix string) (err error) {
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
			fin, err := os.Open(file)
			if err != nil {
				return fmt.Errorf("unable to read %v: %v", file, err)
			}
			defer func() {
				if cerr := fin.Close(); cerr != nil && err == nil {
					err = fmt.Errorf("when closing %v: %v", file, cerr)
				}
			}()

			fout, err := os.Create(targetFile)
			if err != nil {
				return fmt.Errorf("unable to create %v: %v", targetFile, err)
			}
			defer func() {
				if cerr := fout.Close(); cerr != nil && err == nil {
					err = fmt.Errorf("when closing %v: %v", targetFile, cerr)
				}
			}()

			if _, err = io.Copy(fout, fin); err != nil {
				return fmt.Errorf("unable to copy %v to %v: %v", file, targetFile, err)
			}
			if err = fout.Sync(); err != nil {
				return fmt.Errorf("when syncing %v: %v", targetFile, err)
			}

		default:
			return fmt.Errorf("unknown operation %s", op)
		}
	}

	return nil
}

// frameworkOp perform the given operation (either Install or Remove) on the
// given package that's installed in the given path.
func frameworkOp(op policyOp, pkgName string, instPath string) error {
	pol := filepath.Join(instPath, "meta", "framework-policy")
	for _, i := range []string{"apparmor", "seccomp"} {
		for _, j := range []string{"policygroups", "templates"} {
			if err := iterOp(op, filepath.Join(pol, i, j, "*"), filepath.Join(SecBase, i, j), pkgName+"_"); err != nil {
				return err
			}
		}
	}

	return nil
}

// Install sets up the framework's policy from the given snap that's
// installed in the given path.
func Install(pkgName string, instPath string) error {
	return frameworkOp(install, pkgName, instPath)
}

// Remove cleans up the framework's policy from the given snap that's
// installed in the given path.
func Remove(pkgName string, instPath string) error {
	return frameworkOp(remove, pkgName, instPath)
}

func aaUp(old, suf, new, dir string) map[string]bool {
	return helpers.DirUpdated(filepath.Join(old, dir), suf, filepath.Join(new, dir))
}

// AppArmorDelta returns which policies and templates are updated in the
// package at newPath, as compared to those installed in the system.
func AppArmorDelta(pkgName string, newPath string) (policies map[string]bool, templates map[string]bool) {
	newaa := filepath.Join(newPath, "meta", "framework-policy", "apparmor")
	oldaa := filepath.Join(SecBase, "apparmor")

	suf := pkgName + "_"

	return aaUp(oldaa, suf, newaa, "policygroups"), aaUp(oldaa, suf, newaa, "templates")
}
