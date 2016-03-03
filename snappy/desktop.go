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

package snappy

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

// this is a bit confusing, a localestring in xdg is:
// "key=value" or "key[locale]=" with locale as:
//  lang_COUNTRY@MODFIER (or a subset of this)
var desktopFileI18nPattern = `(|\[[a-zA-Z_@]+\])`
var validDesktopFileLines = []*regexp.Regexp{
	// headers
	regexp.MustCompile(`^\[Desktop Entry\]$`),
	// the spec says Action is [a-zA-Z0-9]+ but the real world
	// disagrees and has at least "-" as a common char
	regexp.MustCompile(`^\[Desktop Action [a-zA-Z0-9-]+\]$`),
	// whitespace lines
	regexp.MustCompile(`^\s*$`),
	// lines with comments
	regexp.MustCompile(`^\s*#`),
	// https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s05.html
	regexp.MustCompile(`^Type=`),
	regexp.MustCompile(`^Version=`),
	regexp.MustCompile(fmt.Sprintf(`^Name%s=`, desktopFileI18nPattern)),
	regexp.MustCompile(fmt.Sprintf(`^GenericName%s=`, desktopFileI18nPattern)),
	regexp.MustCompile(`^NoDisplay=`),
	regexp.MustCompile(fmt.Sprintf(`^Comment%s=`, desktopFileI18nPattern)),
	regexp.MustCompile(`^Icon=`),
	regexp.MustCompile(`^Hidden=`),
	regexp.MustCompile(`^OnlyShowIn=`),
	regexp.MustCompile(`^NotShowIn=`),
	regexp.MustCompile(`^Exec=`),
	// Note that we do not support TryExec, it does not make sense
	// in the snap context
	regexp.MustCompile(`^Terminal=`),
	regexp.MustCompile(`^Actions=`),
	regexp.MustCompile(`^MimeType=`),
	regexp.MustCompile(`^Categories=`),
	regexp.MustCompile(fmt.Sprintf(`^Keywords%s=`, desktopFileI18nPattern)),
	regexp.MustCompile(`^StartupNotify=`),
	regexp.MustCompile(`^StartupWMClass`),
}

func isValidDesktopFilePrefix(line string) bool {
	for _, re := range validDesktopFileLines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func rewriteExecLine(m *snapYaml, line string) (string, error) {
	cmd := strings.SplitN(line, "=", 2)[1]
	for _, app := range m.Apps {
		validCmd := filepath.Base(generateBinaryName(m, app))
		// check the prefix to allow %flag style args
		// this is ok because desktop files are not run through sh
		// so we don't have to worry about the arguments too much
		if cmd == validCmd || strings.HasPrefix(cmd, validCmd+" ") {
			binDir := stripGlobalRootDir(dirs.SnapBinariesDir)
			absoluteCmd := filepath.Join(binDir, cmd)
			return strings.Replace(line, cmd, absoluteCmd, 1), nil
		}
	}

	return "", fmt.Errorf("invalid exec command: %q", cmd)
}

func sanitizeDesktopFile(m *snapYaml, realBaseDir string, rawcontent []byte) []byte {
	newContent := []string{}

	scanner := bufio.NewScanner(bytes.NewReader(rawcontent))
	for scanner.Scan() {
		line := scanner.Text()
		// ignore everything we have not whitelisted
		if !isValidDesktopFilePrefix(line) {
			continue
		}
		// rewrite exec lines to an absolute path for the binary
		if strings.HasPrefix(line, "Exec=") {
			var err error
			line, err = rewriteExecLine(m, line)
			if err != nil {
				// something went wrong, ignore the line
				continue
			}
		}

		// do variable substitution
		line = strings.Replace(line, "${SNAP}", realBaseDir, -1)
		newContent = append(newContent, line)
	}

	return []byte(strings.Join(newContent, "\n"))
}

func addPackageDesktopFiles(m *snapYaml, baseDir string) error {
	if err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755); err != nil {
		return err
	}

	desktopFiles, err := filepath.Glob(filepath.Join(baseDir, "meta", "gui", "*.desktop"))
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", baseDir, err)
	}

	for _, df := range desktopFiles {
		content, err := ioutil.ReadFile(df)
		if err != nil {
			return err
		}

		realBaseDir := stripGlobalRootDir(baseDir)
		content = sanitizeDesktopFile(m, realBaseDir, content)

		installedDesktopFileName := filepath.Join(dirs.SnapDesktopFilesDir, fmt.Sprintf("%s_%s", m.Name, filepath.Base(df)))
		if err := osutil.AtomicWriteFile(installedDesktopFileName, []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

func removePackageDesktopFiles(m *snapYaml) error {
	glob := filepath.Join(dirs.SnapDesktopFilesDir, m.Name+"_*.desktop")
	activeDesktopFiles, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", glob, err)
	}
	for _, f := range activeDesktopFiles {
		os.Remove(f)
	}

	return nil
}
