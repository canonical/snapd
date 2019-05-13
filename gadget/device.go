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
package gadget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var ErrDeviceNotFound = errors.New("device not found")

// FindDeviceForStructure attempts to find an existing device matching given
// volume structure, by inspecting its name and, optionally, the filesystem
// label. Assumes that the host's udev has set up device symlinks correctly.
func FindDeviceForStructure(ps *PositionedStructure) (string, error) {
	var candidates []string

	if ps.Name != "" {
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", encodeLabel(ps.Name))
		candidates = append(candidates, byPartlabel)
	}

	if ps.Label != "" {
		byFsLabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", encodeLabel(ps.Label))
		candidates = append(candidates, byFsLabel)
	}

	var found string
	var match string
	for _, candidate := range candidates {
		if !osutil.FileExists(candidate) {
			continue
		}
		target, err := os.Readlink(candidate)
		if err != nil {
			return "", fmt.Errorf("cannot read device link: %v", err)
		}
		if found != "" && target != found {
			// partition and filesystem label links point to
			// different devices
			return "", fmt.Errorf("conflicting device match, %q points to %q, previous match %q points to %q",
				candidate, target, match, found)
		}
		found = target
		match = candidate
	}

	if found == "" {
		return "", ErrDeviceNotFound
	}

	return found, nil
}

// encodeLabel encodes a name for use a partition or filesystem label symlink by
// udev. The result matches the output of blkid_encode_string().
func encodeLabel(in string) string {
	const allowed = `#+-.:=@_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

	buf := &bytes.Buffer{}

	for _, r := range in {
		switch {
		case utf8.RuneLen(r) > 1:
			buf.WriteRune(r)
		case r == '\\':
			fallthrough
		case strings.IndexRune(allowed, r) == -1:
			fmt.Fprintf(buf, `\x%x`, r)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
