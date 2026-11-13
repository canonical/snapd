// -*- Mode: Go; indent-tabs-mode: t; tab-width: 4 -*-

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

package ebpf

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/cilium/ebpf"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/naming"
)

// DeviceKey is the key structure describing a device which the snap is allowed
// access to. The Minor and Major keys can have special value AccessAny,
// indicating that any major or minor value matches the key.
//
// It must match struct sc_cgroup_v2_device_key in device-cgroup-support.c.
type DeviceKey struct {
	Type  uint8  // 'c' or 'b'
	Major uint32 // device major number, in host byte order
	Minor uint32 // device minor number, in host byte order
}

// AccessAny special value matching any major or minor value.
const AccessAny = math.MaxUint32

// MarshalBytes encodes the device key in the packed format expected by the BPF
// map (9 bytes, matching __attribute__((packed)) in C). Implements
// encoding.BinaryMarshaler.
func (k *DeviceKey) MarshalBinary() ([]byte, error) {
	buf := make([]byte, DeviceKeySize)
	buf[0] = k.Type

	// TODO:GOVERSION:use binary.NativeEndian
	e := arch.Endian()
	e.PutUint32(buf[1:5], k.Major)
	e.PutUint32(buf[5:9], k.Minor)
	return buf, nil
}

// UnmarshalBytes decodes the device key from the packed BPF map format.
// Implements encoding.BinaryUnmarshaler.
func (k *DeviceKey) UnmarshalBinary(data []byte) error {

	if l := len(data); l < DeviceKeySize {
		return fmt.Errorf("cannot unmarshal device key: unexpected size %v", l)
	}

	// TODO:GOVERSION:use binary.NativeEndian
	e := arch.Endian()

	k.Type = data[0]
	k.Major = e.Uint32(data[1:5])
	k.Minor = e.Uint32(data[5:9])
	return nil
}

// DeviceKeySize is the size of a packed DeviceKey.
const DeviceKeySize = 9

// SecurityTagToBPFPath converts a snap security tag (e.g. "snap.foo.bar")
// to the corresponding BPF map pin path, replacing dots with underscores.
func SecurityTagToBPFPath(securityTag string) string {
	tag := strings.ReplaceAll(securityTag, ".", "_")
	return fmt.Sprintf("%s/%s", dirs.SnapBPFFSDir, tag)
}

// DeviceMap wraps the underlying eBPF map capturing device access permissions.
type DeviceMap struct {
	m *ebpf.Map
}

// LoadDeviceMap opens the pinned BPF device hash map for the given
// security tag. The caller is responsible for closing the returned map.
func LoadDeviceMap(securityTag string) (*DeviceMap, error) {
	path := SecurityTagToBPFPath(securityTag)
	m, err := ebpf.LoadPinnedMap(path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot load device map at %s: %v", path, err)
	}
	return &DeviceMap{m: m}, nil
}

// Close the map.
func (d *DeviceMap) Close() error {
	return d.m.Close()
}

// Iterate over all entries in the BPF device hash map and calls fn for each
// key. The iteration stops if fn returns an error.
func (d *DeviceMap) Iterate(fn func(key DeviceKey) error) error {
	iter := d.m.Iterate()
	keyBuf := make([]byte, DeviceKeySize)
	valBuf := make([]byte, 1) // value is uint8, always 1
	for iter.Next(&keyBuf, &valBuf) {
		var key DeviceKey
		if err := key.UnmarshalBinary(keyBuf); err != nil {
			return err
		}
		if err := fn(key); err != nil {
			return err
		}
	}
	return iter.Err()
}

// bpffsPinnedNameToSecurityTag converts the name of a pinned device map
// file to the security tag it was created for. The encoding, performed
// by snap-confine and mirrored by SecurityTagToBPFPath, replaces the dots
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
// The converter guarantees the structure of the tag (non-empty,
// dot-separated parts) but not its content; use naming.ParseSecurityTag
// to validate the content (charsets, instance-key rules) and to inspect
// its parts. An error is returned only for names that cannot
// structurally be pins (no "snap_" prefix, fewer than two
// underscore-separated segments after it, or an empty segment).
func bpffsPinnedNameToSecurityTag(name string) (string, error) {
	cannotIdentifyErr := fmt.Errorf("cannot identify security tag from name %q", name)
	if !strings.HasPrefix(name, "snap_") {
		return "", cannotIdentifyErr
	}
	segments := strings.Split(name[len("snap_"):], "_")
	if len(segments) < 2 {
		return "", cannotIdentifyErr
	}
	for _, segment := range segments {
		if segment == "" {
			return "", cannotIdentifyErr
		}
	}
	if len(segments) >= 3 && segments[len(segments)-2] == "hook" {
		// snap_<instance>_hook_<hookname>
		instance := strings.Join(segments[:len(segments)-2], "_")
		hookName := segments[len(segments)-1]
		if err := naming.ValidateHook(hookName); err == nil {
			return fmt.Sprintf("snap.%s.hook.%s", instance, hookName), nil
		}
	}
	// snap_<instance>_<app> (the app may be literally named "hook")
	instance := strings.Join(segments[:len(segments)-1], "_")
	return fmt.Sprintf("snap.%s.%s", instance, segments[len(segments)-1]), nil
}

// FindActiveDeviceMapTagsForSnap returns the security tags of the device
// maps pinned for the given snap instance name, sorted. Only tags
// belonging to the exact instance are returned; in particular the maps
// of parallel installs of the same snap are excluded. Tags of apps,
// snap hooks and component hooks of this instance are included. It
// returns nil if there is nothing to find.
func FindActiveDeviceMapTagsForSnap(instanceName string) (tags []string, err error) {
	entries, err := os.ReadDir(dirs.SnapBPFFSDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %v", dirs.SnapBPFFSDir, err)
	}
	for _, e := range entries {
		tag, err := bpffsPinnedNameToSecurityTag(e.Name())
		if err != nil {
			continue
		}
		parsedTag, err := naming.ParseSecurityTag(tag)
		if err != nil {
			continue
		}
		if parsedTag.InstanceName() != instanceName {
			continue
		}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}
