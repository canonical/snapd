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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image/preseed"
)

// sysfsRootDir is the root from which sysfs paths are resolved. It is a
// variable so that tests can point it at a temporary directory.
var sysfsRootDir = "/"

type cmdGenerateSysfsOverlay struct {
	Positional struct {
		OverlayDir string `positional-arg-name:"<overlay-dir>"`
	} `positional-args:"yes" required:"yes"`
}

var shortGenerateSysfsOverlayHelp = i18n.G("Generate a sysfs overlay for use with prepare-image")
var longGenerateSysfsOverlayHelp = i18n.G(`
The generate-sysfs-overlay command creates a skeleton mirror of the current
machine's sysfs hardware entries in the specified directory. The resulting
directory can then be passed to the snap prepare-image --sysfs-overlay option,
enabling preseeding of an image on a build host that differs from the target
device.

The overlay captures /sys/class entries (such as /sys/class/gpio,
/sys/class/leds, /sys/class/pwm, etc.) that are relevant to snap interface
security profile generation. The corresponding /sys/devices/platform paths
are included automatically as the real backing targets of the class symlinks.

If the target directory already exists, the command exits with an error.
Remove it first before regenerating.`)

func init() {
	addCommand("generate-sysfs-overlay",
		shortGenerateSysfsOverlayHelp,
		longGenerateSysfsOverlayHelp,
		func() flags.Commander { return &cmdGenerateSysfsOverlay{} },
		nil,
		[]argDesc{
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<overlay-dir>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("Path to the directory to generate the sysfs overlay in"),
			},
		})
}

func (x *cmdGenerateSysfsOverlay) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	overlayDir := x.Positional.OverlayDir

	if _, err := os.Lstat(overlayDir); err == nil {
		return fmt.Errorf("overlay directory %q already exists, remove it first", overlayDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot stat overlay directory %q: %v", overlayDir, err)
	}

	return generateSysfsOverlay(overlayDir)
}

// generateSysfsOverlay creates overlayDir and populates it with a skeleton
// mirror of the /sys/class/* entries from preseed.PermittedSysfsOverlays.
// Only sys/class/* paths are traversed directly; the corresponding
// /sys/devices/platform/... backing paths are captured automatically as the
// real targets of the class symlinks, exactly as the target device exposes them.
func generateSysfsOverlay(overlayDir string) error {
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		return fmt.Errorf("cannot create overlay directory %q: %v", overlayDir, err)
	}

	for _, relPath := range preseed.PermittedSysfsOverlays {
		if !strings.HasPrefix(relPath, "sys/class/") {
			continue
		}
		activeDir := filepath.Join(sysfsRootDir, relPath)
		if _, err := os.Lstat(activeDir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("cannot stat %q: %v", activeDir, err)
		}

		if err := populateSysfsDirectory(overlayDir, activeDir); err != nil {
			return err
		}
	}

	return nil
}

// sysfsOverlayPath returns the path within overlayDir that mirrors the
// absolute sysfs path p, by direct string concatenation followed by Clean.
func sysfsOverlayPath(overlayDir, p string) string {
	return filepath.Clean(overlayDir + p)
}

// populateSysfsDirectory mirrors activeDir into overlayDir.
//
// For each symlink in activeDir:
//   - Creates the symlink at its original path in the overlay (same link target).
//   - Mirrors the directory structure of the symlink's real target
//     (subdirectories, regular files, and sub-symlinks, one level deep).
//
// For regular files directly in activeDir: creates empty stub files at their
// realpath within the overlay.
//
// For subdirectories directly in activeDir: creates them at their realpath in
// the overlay and stubs their direct files.
func populateSysfsDirectory(overlayDir, activeDir string) error {
	if err := os.MkdirAll(sysfsOverlayPath(overlayDir, activeDir), 0755); err != nil {
		return fmt.Errorf("cannot create overlay dir %q: %v", activeDir, err)
	}

	entries, err := os.ReadDir(activeDir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", activeDir, err)
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}

		entPath := filepath.Join(activeDir, e.Name())

		// Resolve the symlink to its canonical real target.
		realTarget, err := filepath.EvalSymlinks(entPath)
		if err != nil {
			// Real target may not exist on this machine; skip gracefully.
			continue
		}

		// Raw symlink target (may be relative), preserved as-is.
		linkTarget, err := os.Readlink(entPath)
		if err != nil {
			return fmt.Errorf("cannot read symlink %q: %v", entPath, err)
		}

		// Create both the real target directory and the symlink's parent
		// inside the overlay.
		if err := os.MkdirAll(sysfsOverlayPath(overlayDir, realTarget), 0755); err != nil {
			return fmt.Errorf("cannot create overlay dir for %q: %v", realTarget, err)
		}
		if err := os.MkdirAll(sysfsOverlayPath(overlayDir, filepath.Dir(entPath)), 0755); err != nil {
			return fmt.Errorf("cannot create overlay dir for %q: %v", filepath.Dir(entPath), err)
		}

		// Recreate the symlink at its original path inside the overlay.
		symlinkDst := sysfsOverlayPath(overlayDir, entPath)
		if err := os.Symlink(linkTarget, symlinkDst); err != nil && !os.IsExist(err) {
			return fmt.Errorf("cannot create overlay symlink %q: %v", symlinkDst, err)
		}

		// Mirror the real target: its subdirectories, direct files, and
		// direct sub-symlinks (one level deep each).
		if err := mimicSysfsDirs(overlayDir, realTarget); err != nil {
			return err
		}
		if err := mimicSysfsFiles(overlayDir, realTarget); err != nil {
			return err
		}
		if err := mimicSysfsLinks(overlayDir, realTarget); err != nil {
			return err
		}
	}

	// Handle regular files and subdirectories directly in activeDir.
	if err := mimicSysfsFiles(overlayDir, activeDir); err != nil {
		return err
	}
	return mimicSysfsDirs(overlayDir, activeDir)
}

// mimicSysfsFiles creates empty stub files in the overlay for each regular
// file found one level deep in activeDir, placed at their resolved realpath.
func mimicSysfsFiles(overlayDir, activeDir string) error {
	entries, err := os.ReadDir(activeDir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", activeDir, err)
	}

	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}

		entPath := filepath.Join(activeDir, e.Name())
		realPath, err := filepath.EvalSymlinks(entPath)
		if err != nil {
			continue
		}

		dst := sysfsOverlayPath(overlayDir, realPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("cannot create overlay dir for %q: %v", filepath.Dir(dst), err)
		}

		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL, 0644)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("cannot create overlay stub file %q: %v", dst, err)
		}
		if f != nil {
			f.Close()
		}
	}

	return nil
}

// mimicSysfsLinks recreates each symlink found one level deep in activeDir
// inside the overlay at its original path with the same link target.
func mimicSysfsLinks(overlayDir, activeDir string) error {
	entries, err := os.ReadDir(activeDir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", activeDir, err)
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}

		entPath := filepath.Join(activeDir, e.Name())
		linkTarget, err := os.Readlink(entPath)
		if err != nil {
			return fmt.Errorf("cannot read symlink %q: %v", entPath, err)
		}

		dst := sysfsOverlayPath(overlayDir, entPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("cannot create overlay dir for %q: %v", filepath.Dir(dst), err)
		}

		if err := os.Symlink(linkTarget, dst); err != nil && !os.IsExist(err) {
			return fmt.Errorf("cannot create overlay symlink %q: %v", dst, err)
		}
	}

	return nil
}

// mimicSysfsDirs creates directories found one level deep in activeDir inside
// the overlay at their resolved realpath, then stubs their direct files.
func mimicSysfsDirs(overlayDir, activeDir string) error {
	entries, err := os.ReadDir(activeDir)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %v", activeDir, err)
	}

	for _, e := range entries {
		if !e.Type().IsDir() {
			continue
		}

		entPath := filepath.Join(activeDir, e.Name())
		realPath, err := filepath.EvalSymlinks(entPath)
		if err != nil {
			continue
		}

		if err := os.MkdirAll(sysfsOverlayPath(overlayDir, realPath), 0755); err != nil {
			return fmt.Errorf("cannot create overlay dir for %q: %v", realPath, err)
		}

		if err := mimicSysfsFiles(overlayDir, realPath); err != nil {
			return err
		}
	}

	return nil
}
