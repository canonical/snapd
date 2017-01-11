// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// From the freedesktop Desktop Entry Specification¹,
//
//    Keys with type localestring may be postfixed by [LOCALE], where
//    LOCALE is the locale type of the entry. LOCALE must be of the form
//    lang_COUNTRY.ENCODING@MODIFIER, where _COUNTRY, .ENCODING, and
//    @MODIFIER may be omitted. If a postfixed key occurs, the same key
//    must be also present without the postfix.
//
//    When reading in the desktop entry file, the value of the key is
//    selected by matching the current POSIX locale for the LC_MESSAGES
//    category against the LOCALE postfixes of all occurrences of the
//    key, with the .ENCODING part stripped.
//
// sadly POSIX doesn't mention what values are valid for LC_MESSAGES,
// beyond mentioning² that it's implementation-defined (and can be of
// the form [language[_territory][.codeset][@modifier]])
//
// 1. https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s04.html
// 2. http://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap08.html#tag_08_02
//
// So! The following is simplistic, and based on the contents of
// PROVIDED_LOCALES in locales.config, and does not cover all of
// "locales -m" (and ignores XSetLocaleModifiers(3), which may or may
// not be related). Patches welcome, as long as it's readable.
//
// REVIEWERS: this could also be left as `(?:\[[@_.A-Za-z-]\])?=` if even
// the following is hard to read:
const localizedSuffix = `(?:\[[a-z]+(?:_[A-Z]+)?(?:\.[0-9A-Z-]+)?(?:@[a-z]+)?\])?=`

var isValidDesktopFileLine = regexp.MustCompile(strings.Join([]string{
	// NOTE (mostly to self): as much as possible keep the
	// individual regexp simple, optimize for legibility
	//
	// empty lines and comments
	`^\s*$`,
	`^\s*#`,
	// headers
	`^\[Desktop Entry\]$`,
	`^\[Desktop Action [0-9A-Za-z-]+\]$`,
	`^\[[A-Za-z0-9-]+ Shortcut Group\]$`,
	// https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s05.html
	"^Type=",
	"^Version=",
	"^Name" + localizedSuffix,
	"^GenericName" + localizedSuffix,
	"^NoDisplay=",
	"^Comment" + localizedSuffix,
	"^Icon=",
	"^Hidden=",
	"^OnlyShowIn=",
	"^NotShowIn=",
	"^Exec=",
	// Note that we do not support TryExec, it does not make sense
	// in the snap context
	"^Terminal=",
	"^Actions=",
	"^MimeType=",
	"^Categories=",
	"^Keywords" + localizedSuffix,
	"^StartupNotify=",
	"^StartupWMClass=",
	// unity extension
	"^X-Ayatana-Desktop-Shortcuts=",
	"^TargetEnvironment=",
}, "|")).MatchString

// rewriteExecLine rewrites a "Exec=" line to use the wrapper path for snap application.
func rewriteExecLine(s *snap.Info, desktopFile, line string) (string, error) {
	cmd := strings.SplitN(line, "=", 2)[1]
	for _, app := range s.Apps {
		env := fmt.Sprintf("env BAMF_DESKTOP_FILE_HINT=%s ", desktopFile)
		wrapper := app.WrapperPath()
		validCmd := filepath.Base(wrapper)
		// check the prefix to allow %flag style args
		// this is ok because desktop files are not run through sh
		// so we don't have to worry about the arguments too much
		if cmd == validCmd {
			return "Exec=" + env + wrapper, nil
		} else if strings.HasPrefix(cmd, validCmd+" ") {
			return fmt.Sprintf("Exec=%s%s%s", env, wrapper, line[len("Exec=")+len(validCmd):]), nil
		}
	}

	return "", fmt.Errorf("invalid exec command: %q", cmd)
}

func sanitizeDesktopFile(s *snap.Info, desktopFile string, rawcontent []byte) []byte {
	newContent := []string{}

	scanner := bufio.NewScanner(bytes.NewReader(rawcontent))
	for i := 0; scanner.Scan(); i++ {
		line := scanner.Text()

		if !isValidDesktopFileLine(line) {
			logger.Debugf("ignoring line %d (%q) in source of desktop file %q", i, line, filepath.Base(desktopFile))
			continue
		}

		// rewrite exec lines to an absolute path for the binary
		if strings.HasPrefix(line, "Exec=") {
			var err error
			line, err = rewriteExecLine(s, desktopFile, line)
			if err != nil {
				// something went wrong, ignore the line
				continue
			}
		}

		// do variable substitution
		line = strings.Replace(line, "${SNAP}", s.MountDir(), -1)
		newContent = append(newContent, line)
	}

	return []byte(strings.Join(newContent, "\n"))
}

func updateDesktopDatabase(desktopFiles []string) error {
	if len(desktopFiles) == 0 {
		return nil
	}

	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		if output, err := exec.Command("update-desktop-database", dirs.SnapDesktopFilesDir).CombinedOutput(); err != nil {
			return fmt.Errorf("cannot update-desktop-database %q: %s", output, err)
		}
		logger.Debugf("update-desktop-database successful")
	}
	return nil
}

// AddSnapDesktopFiles puts in place the desktop files for the applications from the snap.
func AddSnapDesktopFiles(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755); err != nil {
		return err
	}

	baseDir := s.MountDir()

	desktopFiles, err := filepath.Glob(filepath.Join(baseDir, "meta", "gui", "*.desktop"))
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", baseDir, err)
	}

	for _, df := range desktopFiles {
		content, err := ioutil.ReadFile(df)
		if err != nil {
			return err
		}

		installedDesktopFileName := filepath.Join(dirs.SnapDesktopFilesDir, fmt.Sprintf("%s_%s", s.Name(), filepath.Base(df)))
		content = sanitizeDesktopFile(s, installedDesktopFileName, content)
		if err := osutil.AtomicWriteFile(installedDesktopFileName, []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	// updates mime info etc
	if err := updateDesktopDatabase(desktopFiles); err != nil {
		return err
	}

	return nil
}

// RemoveSnapDesktopFiles removes the added desktop files for the applications in the snap.
func RemoveSnapDesktopFiles(s *snap.Info) error {
	glob := filepath.Join(dirs.SnapDesktopFilesDir, s.Name()+"_*.desktop")
	activeDesktopFiles, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", glob, err)
	}
	for _, f := range activeDesktopFiles {
		os.Remove(f)
	}

	// updates mime info etc
	if err := updateDesktopDatabase(activeDesktopFiles); err != nil {
		return err
	}

	return nil
}
