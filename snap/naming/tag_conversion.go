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

package naming

import (
	"fmt"
	"strings"
)

// SecurityTagToBPFMapName converts a security tag to its BPF map pin name,
// replacing dots with underscores and preserving the component separator '+'.
// The tag must already be valid. This mirrors the encoding in
// cmd/libsnap-confine-private/device-cgroup-support.c.
func SecurityTagToBPFMapName(tag string) string {
	return strings.ReplaceAll(tag, ".", "_")
}

// BPFMapNameToSecurityTag converts the name of a pinned device map
// file to the security tag it was created for. The encoding, performed
// by snap-confine and mirrored by SecurityTagToBPFMapName, replaces the dots
// of the security tag with underscores; the plus sign separating a snap
// instance from a component name is preserved.
//
// Because the underscore is also the separator between a snap name and
// its instance key, one shape is ambiguous: snap_<instance>_hook_<hookname>
// is also a valid pin of the app <hookname> of the instance whose key is
// "hook". When <hookname> is both a valid app name and a valid hook name,
// prefer the hook interpretation, following the snap-device-helper
// convention. If it is an invalid hook name, return the app interpretation.
//
// The converter uses ParseSecurityTag to validate the content (charsets,
// instance-key rules) and returns a parsed security tag so callers can inspect
// its parts. An error is returned for names that cannot structurally be pins
// (no "snap_" prefix, fewer than two underscore-separated segments after it,
// or an empty segment), or whose converted tag is invalid.
func BPFMapNameToSecurityTag(name string) (SecurityTag, error) {
	cannotIdentifyErr := fmt.Errorf("cannot identify security tag from name %q", name)
	prefix, rest, ok := strings.Cut(name, "_")
	if !ok || prefix != "snap" {
		return nil, cannotIdentifyErr
	}

	// App and hook names cannot contain underscores, so the final sep
	// always separates the runnable name from the instance (and hook marker).
	sep := strings.LastIndexByte(rest, '_')
	if sep < 0 {
		return nil, cannotIdentifyErr
	}
	instance, runnable := rest[:sep], rest[sep+1:]
	tag := fmt.Sprintf("snap.%s.%s", instance, runnable)

	// Resolve the only ambiguous form explicitly.
	if strings.HasSuffix(instance, "_hook") && ValidateHook(runnable) == nil {
		instance = strings.TrimSuffix(instance, "_hook")
		tag = fmt.Sprintf("snap.%s.hook.%s", instance, runnable)
	}

	parsed, err := ParseSecurityTag(tag)
	if err != nil {
		return nil, cannotIdentifyErr
	}
	return parsed, nil
}
