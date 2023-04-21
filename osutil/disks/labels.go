// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package disks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
)

// BlkIDEncodeLabel encodes a name for use as a partition or filesystem
// label symlink by udev. The result matches the output of blkid_encode_string()
// from libblkid.
func BlkIDEncodeLabel(in string) string {
	const allowed = `#+-.:=@_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

	buf := &bytes.Buffer{}

	for _, r := range in {
		switch {
		case utf8.RuneLen(r) > 1:
			buf.WriteRune(r)
		case !strings.ContainsRune(allowed, r):
			fmt.Fprintf(buf, `\x%02x`, r)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

var hexCode = regexp.MustCompile(`\\x[0-9a-f]{2}`)

// BlkIDDecodeLabel decodes a string such as a filesystem or partition label
// encoded by udev in BlkIDEncodeLabel for normal comparison, i.e.
// "BIOS\x20Boot" becomes "BIOS Boot"
func BlkIDDecodeLabel(in string) (string, error) {
	out := strings.Builder{}
	pos := 0
	for _, m := range hexCode.FindAllStringIndex(in, -1) {
		start := m[0]
		beforeMatch := in[pos:start]
		if i := strings.IndexRune(beforeMatch, '\\'); i >= 0 {
			return "", fmt.Errorf(`string is malformed, unparsable escape sequence at "%s"`, beforeMatch[i:])
		}
		out.WriteString(beforeMatch)
		hex := in[start+2 : start+4]
		n, err := strconv.ParseUint(hex, 16, 8)
		if err != nil {
			// This cannot really happen, since the regexp wouldn't match otherwise
			return "", fmt.Errorf("internal error: cannot parse hex %q despite matching regexp", hex)
		}
		out.WriteRune(rune(n))
		pos = m[1]
	}
	remaining := in[pos:]
	if i := strings.IndexRune(remaining, '\\'); i >= 0 {
		return "", fmt.Errorf(`string is malformed, unparsable escape sequence at "%s"`, remaining[i:])
	}
	out.WriteString(remaining)
	return out.String(), nil
}

// CandidateByLabelPath searches for a filesystem with label matching
// "label". It tries first an exact match, otherwise it tries again by
// ignoring capitalization, but in that case it will return a match
// only if the filesystem is vfat. If found, it returns the path of
// the symlink in the by-label folder.
func CandidateByLabelPath(label string) (string, error) {
	byLabelDir := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/")
	byLabelFs, err := os.ReadDir(byLabelDir)
	if err != nil {
		return "", err
	}
	candidate := ""
	// encode it so it can be compared with the files
	label = BlkIDEncodeLabel(label)
	// Search first for an exact match
	for _, file := range byLabelFs {
		if file.Name() == label {
			candidate = file.Name()
			logger.Debugf("found candidate %q for gadget label %q",
				candidate, label)
			break
		}
	}
	if candidate == "" {
		// Now try to find a candidate ignoring case, which
		// will be fine only for vfat partitions.
		labelLow := strings.ToLower(label)
		for _, file := range byLabelFs {
			if strings.ToLower(file.Name()) == labelLow {
				if candidate != "" {
					return "", fmt.Errorf("more than one candidate for label %q", label)
				}
				candidate = file.Name()
			}
		}
		if candidate == "" {
			return "", fmt.Errorf("no candidate found for label %q", label)
		}
		// Make sure it is vfat
		fsType, err := filesystemTypeForPartition(filepath.Join(byLabelDir, candidate))
		if err != nil {
			return "", fmt.Errorf("cannot find filesystem type: %v", err)
		}
		if fsType != "vfat" {
			return "", fmt.Errorf("no candidate found for label %q (%q is not vfat)", label, candidate)
		}
		logger.Debugf("found candidate %q (vfat) for gadget label %q",
			candidate, label)
	}

	return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", candidate), nil
}
