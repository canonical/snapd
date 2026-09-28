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
	"github.com/cilium/ebpf/ringbuf"

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

// DeviceDenyEvent is the structure written to the ring buffer by the BPF
// program when a device access is denied. It must match struct
// sc_device_deny_event in device-cgroup-support.h.
type DeviceDenyEvent struct {
	DevType   uint8  // 'c' or 'b'
	Access    uint8  // BPF_DEVCG_ACC_* bitmask (1=mknod, 2=read, 4=write)
	_pad      uint16 //nolint:unused
	Major     uint32
	Minor     uint32
	PID       uint32
	Timestamp uint64   // ktime_get_ns()
	Comm      [16]byte // TASK_COMM_LEN
}

// DeviceDenyEventSize is the size of a packed DeviceDenyEvent.
const DeviceDenyEventSize = 40

// CommString returns the process comm as a Go string, trimming null bytes.
func (e *DeviceDenyEvent) CommString() string {
	n := 0
	for n < len(e.Comm) && e.Comm[n] != 0 {
		n++
	}
	return string(e.Comm[:n])
}

// AccessString returns a human-readable representation of the access flags.
func (e *DeviceDenyEvent) AccessString() string {
	var parts []string
	if e.Access&1 != 0 {
		parts = append(parts, "mknod")
	}
	if e.Access&2 != 0 {
		parts = append(parts, "read")
	}
	if e.Access&4 != 0 {
		parts = append(parts, "write")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("0x%x", e.Access)
	}
	return strings.Join(parts, ",")
}

// DecodeDeviceDenyEvent decodes a DeviceDenyEvent from raw bytes.
func DecodeDeviceDenyEvent(data []byte) (*DeviceDenyEvent, error) {
	if len(data) < DeviceDenyEventSize {
		return nil, fmt.Errorf("short event record: %d bytes, want %d", len(data), DeviceDenyEventSize)
	}

	// TODO:GOVERSION:use binary.NativeEndian
	e := arch.Endian()

	ev := &DeviceDenyEvent{
		DevType:   data[0],
		Access:    data[1],
		Major:     e.Uint32(data[4:8]),
		Minor:     e.Uint32(data[8:12]),
		PID:       e.Uint32(data[12:16]),
		Timestamp: e.Uint64(data[16:24]),
	}
	copy(ev.Comm[:], data[24:40])
	return ev, nil
}

// SecurityTagToDeviceDenyLogPath returns the path to the deny log
// ring buffer for the given security tag.
func SecurityTagToDeviceDenyLogPath(securityTag string) string {
	tag := strings.ReplaceAll(securityTag, ".", "_")
	return fmt.Sprintf("%s/%s@denylog", dirs.SnapBPFFSDir, tag)
}

// DiscardPinnedMaps removes the pinned BPF device map and deny ring
// buffer for the given security tag. It is robust: if either file does
// not exist, it logs a message via logf and continues. Returns an error
// only if an actual removal fails for a reason other than the file not
// existing.
func DiscardPinnedMaps(securityTag string, logf func(string, ...any)) error {
	mapPath := SecurityTagToBPFPath(securityTag)
	ringPath := SecurityTagToDeviceDenyLogPath(securityTag)

	var firstErr error
	for _, path := range []string{mapPath, ringPath} {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				logf("pinned object %s does not exist, skipping", path)
			} else {
				logf("cannot remove %s: %v", path, err)
				if firstErr == nil {
					firstErr = fmt.Errorf("cannot remove %s: %v", path, err)
				}
			}
		}
	}
	return firstErr
}

// DeviceDenyLogReader wraps a ringbuf.Reader and ensures the underlying
// ebpf.Map is also closed when the reader is closed. ringbuf.NewReader
// does not take ownership of the map, so we must track it separately.
type DeviceDenyLogReader struct {
	*ringbuf.Reader
	m *ebpf.Map
}

func (r *DeviceDenyLogReader) Close() error {
	err := r.Reader.Close()
	if merr := r.m.Close(); merr != nil && err == nil {
		err = merr
	}
	return err
}

// OpenDeviceDenyLog opens the pinned BPF ring buffer map for the given
// security tag and returns a DeviceDenyLogReader. The caller is responsible
// for closing the returned reader.
func OpenDeviceDenyLog(securityTag string) (*DeviceDenyLogReader, error) {
	path := SecurityTagToDeviceDenyLogPath(securityTag)
	m, err := ebpf.LoadPinnedMap(path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot load deny ring buffer at %s: %v", path, err)
	}
	r, err := ringbuf.NewReader(m)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("cannot create ring buffer reader: %v", err)
	}
	return &DeviceDenyLogReader{Reader: r, m: m}, nil
}
