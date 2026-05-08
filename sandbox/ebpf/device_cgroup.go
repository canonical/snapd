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

// Package ebpf provides helpers for interacting with BPF maps and ring
// buffers used by snap-confine's device cgroup filtering.
package ebpf // import "github.com/snapcore/snapd/sandbox/ebpf"

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"

	"github.com/snapcore/snapd/dirs"
)

const (
	// TaskCommLen matches the kernel's TASK_COMM_LEN.
	TaskCommLen = 16
)

// DeviceKey is the key structure used in the BPF device cgroup hash map.
// It must match struct sc_cgroup_v2_device_key in device-cgroup-support.c.
type DeviceKey struct {
	Type  uint8  // 'c' or 'b'
	Major uint32 // device major number
	Minor uint32 // device minor number, UINT32_MAX means "any"
}

// MarshalBytes encodes the device key in the packed format expected by the
// BPF map (9 bytes, matching __attribute__((packed)) in C).
func (k *DeviceKey) MarshalBytes() []byte {
	buf := make([]byte, 9)
	buf[0] = k.Type
	binary.LittleEndian.PutUint32(buf[1:5], k.Major)
	binary.LittleEndian.PutUint32(buf[5:9], k.Minor)
	return buf
}

// UnmarshalBytes decodes the device key from the packed BPF map format.
func (k *DeviceKey) UnmarshalBytes(data []byte) {
	k.Type = data[0]
	k.Major = binary.LittleEndian.Uint32(data[1:5])
	k.Minor = binary.LittleEndian.Uint32(data[5:9])
}

// DeviceKeySize is the size of a packed DeviceKey.
const DeviceKeySize = 9

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
	ev := &DeviceDenyEvent{
		DevType:   data[0],
		Access:    data[1],
		Major:     binary.LittleEndian.Uint32(data[4:8]),
		Minor:     binary.LittleEndian.Uint32(data[8:12]),
		PID:       binary.LittleEndian.Uint32(data[12:16]),
		Timestamp: binary.LittleEndian.Uint64(data[16:24]),
	}
	copy(ev.Comm[:], data[24:40])
	return ev, nil
}

// SecurityTagToBPFPath converts a snap security tag (e.g. "snap.foo.bar")
// to the corresponding BPF map pin path, replacing dots with underscores.
func SecurityTagToBPFPath(securityTag string) string {
	tag := strings.ReplaceAll(securityTag, ".", "_")
	return fmt.Sprintf("%s/%s", dirs.SnapBPFFSDir, tag)
}

// SecurityTagToDeviceDenyLogPath returns the path to the deny log
// ring buffer for the given security tag.
func SecurityTagToDeviceDenyLogPath(securityTag string) string {
	tag := strings.ReplaceAll(securityTag, ".", "_")
	return fmt.Sprintf("%s/%s@denylog", dirs.SnapBPFFSDir, tag)
}

// LoadDeviceMap opens the pinned BPF device hash map for the given
// security tag. The caller is responsible for closing the returned map.
func LoadDeviceMap(securityTag string) (*ebpf.Map, error) {
	path := SecurityTagToBPFPath(securityTag)
	m, err := ebpf.LoadPinnedMap(path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot load device map at %s: %v", path, err)
	}
	return m, nil
}

// IterateDeviceMap iterates over all entries in the BPF device hash map
// and calls fn for each key. The iteration stops if fn returns an error.
func IterateDeviceMap(m *ebpf.Map, fn func(key DeviceKey) error) error {
	iter := m.Iterate()
	keyBuf := make([]byte, DeviceKeySize)
	valBuf := make([]byte, 1) // value is uint8, always 1
	for iter.Next(&keyBuf, &valBuf) {
		var key DeviceKey
		key.UnmarshalBytes(keyBuf)
		if err := fn(key); err != nil {
			return err
		}
	}
	return iter.Err()
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
