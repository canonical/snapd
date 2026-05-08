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

// bpffsPinnedNameToSecurityTag converts a name of a pinned entry back to a
// security tag. The bpffs name has the format snap_<name>_<app> where dots in
// the original tag were replaced with underscores. Valid snap and app names cannot
// contain underscores.
func bpffsPinnedNameToSecurityTag(name string) (tag string, err error) {
	first := strings.Index(name, "_")
	last := strings.LastIndex(name, "_")
	// Do a lightweight check, the names are reasonably validated before even
	// getting here.
	if first < 0 || last < 0 || (last-first) < 2 {
		return "", fmt.Errorf("cannot identify security tag from name %q", name)
	}
	return name[:first] + "." + name[first+1:last] + "." + name[last+1:], nil
}

func FindActiveDeviceMapsForSnap(instanceName string) (tags []string, err error) {
	// Security tags in bpffs have dots replaced with underscores.
	// Pattern: snap_<name>_<app>
	// This also matches parallel installs (e.g. snap_foo_inst_bar for
	// instance "foo_inst") since snap and app names cannot contain
	// underscores.
	prefix := fmt.Sprintf("snap_%s_", instanceName)
	entries, err := os.ReadDir(dirs.SnapBPFFSDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %v", dirs.SnapBPFFSDir, err)
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) {
			if tag, err := bpffsPinnedNameToSecurityTag(name); err == nil && tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	sort.Strings(tags)
	return tags, nil
}
