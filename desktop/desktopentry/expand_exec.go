// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package desktopentry

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/strutil/shlex"
)

func shellQuote(s string) string {
	return "'" + strings.Replace(s, "'", "'\\''", -1) + "'"
}

func toFilePath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if !u.IsAbs() {
		return "", fmt.Errorf("%q is not an absolute URI", uri)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("%q is not a file URI", uri)
	}
	if !filepath.IsAbs(u.Path) {
		return "", fmt.Errorf("%q does not have an absolute file path", uri)
	}
	return u.Path, nil
}

func expandMacro(r rune, buf *bytes.Buffer, de *DesktopEntry, uris []string) ([]string, error) {
	switch r {
	case 'u':
		if len(uris) > 0 {
			// Format as a file path, falling back to a URI
			arg, err := toFilePath(uris[0])
			if err != nil {
				arg = uris[0]
			}
			buf.WriteString(shellQuote(arg))
			uris = uris[1:]
		}
	case 'U':
		first := true
		for _, u := range uris {
			if !first {
				buf.WriteRune(' ')
			}
			first = false
			// Format as a file path, falling back to a URI
			arg, err := toFilePath(u)
			if err != nil {
				arg = u
			}
			buf.WriteString(shellQuote(arg))
		}
		uris = nil
	case 'f':
		if len(uris) > 0 {
			arg, err := toFilePath(uris[0])
			if err != nil {
				return nil, err
			}
			buf.WriteString(shellQuote(arg))
			uris = uris[1:]
		}
	case 'F':
		first := true
		for _, u := range uris {
			if !first {
				buf.WriteRune(' ')
			}
			first = false
			arg, err := toFilePath(u)
			if err != nil {
				return nil, err
			}
			buf.WriteString(shellQuote(arg))
		}
		uris = nil
	case 'i':
		if de.Icon != "" {
			buf.WriteString("--icon ")
			buf.WriteString(shellQuote(de.Icon))
		}
	case 'c':
		if de.Name != "" {
			buf.WriteString(shellQuote(de.Name))
		}
	case 'k':
		if de.Filename != "" {
			buf.WriteString(shellQuote(de.Filename))
		}
	case '%':
		buf.WriteRune('%')
	}
	return uris, nil
}

// expandExec expands macros within a desktop entry exec variable.
//
// The format is described in the Desktop Entry Specification:
//
//	https://specifications.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#exec-variables
//
// It uses glib implementation as a reference where the spec is
// ambiguous.
func expandExec(de *DesktopEntry, exec string, uris []string) (args []string, err error) {
	var buf bytes.Buffer
	isMacro := false
	nUris := len(uris)
	for _, r := range exec {
		if isMacro {
			if uris, err = expandMacro(r, &buf, de, uris); err != nil {
				return nil, err
			}
			isMacro = false
		} else if r == '%' {
			isMacro = true
		} else {
			buf.WriteRune(r)
		}
	}
	// If no URI substitutions have happened, add a single
	// implicit %f argument
	if len(uris) > 0 && len(uris) == nUris {
		buf.WriteRune(' ')
		if _, err = expandMacro('f', &buf, de, uris); err != nil {
			return nil, err
		}
	}

	return shlex.Split(buf.String())
}
