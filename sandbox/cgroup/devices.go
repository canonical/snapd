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

package cgroup

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/ebpf"
	"github.com/snapcore/snapd/snap/naming"
)

// deviceMapAccessor abstracts iteration over device map entries.
type deviceMapAccessor interface {
	Iterate(fn func(ebpf.DeviceKey) error) error
	Close() error
}

var (
	loadDeviceMapFunc                   = loadDeviceMapImpl
	findActiveDeviceMapTagsForSnapFunc  = ebpf.FindActiveDeviceMapTagsForSnap
	findActiveDeviceMapPathsForSnapFunc = ebpf.FindActiveDeviceMapPathsForSnap
)

func loadDeviceMapImpl(securityTag string) (deviceMapAccessor, error) {
	return ebpf.LoadDeviceMap(securityTag)
}

// FindActiveDeviceMediationForSnap finds all security tags for a given
// snap name which have active device mediation.
func FindActiveDeviceMediationForSnap(snapName string) (tags []string, err error) {
	v, err := Version()
	if err != nil {
		return nil, err
	}
	if v == V2 {
		return findActiveDeviceMapTagsForSnapFunc(snapName)
	}
	return findSecurityTagsV1(snapName)
}

func findSecurityTagsV1(snapName string) ([]string, error) {
	cgroupDevicesPath := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/devices")
	prefix := fmt.Sprintf("snap.%s.", snapName)
	entries, err := os.ReadDir(cgroupDevicesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", cgroupDevicesPath, err)
	}
	var tags []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			tags = append(tags, e.Name())
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// AccessAny is the special value matching any major or minor value.
const AccessAny = math.MaxUint32 // same value as ebpf.AccessAny

// DeviceEntry represents a single device entry to which the snap's access is
// mediated.
type DeviceEntry struct {
	DevType byte   // 'c' (character), 'b' (block), or 'a' (any)
	Major   uint32 // major number
	Minor   uint32 // minor number
	Access  string // e.g. "rwm"
}

// ListMediatedDevicesForSecurityTag returns the list of mediated device access
// for a given security tag.
func ListMediatedDevicesForSecurityTag(tag string) ([]DeviceEntry, error) {
	v, err := Version()
	if err != nil {
		return nil, err
	}
	if v == V2 {
		return collectDevicesV2(tag)
	}
	return collectDevicesV1(tag)
}

func collectDevicesV2(securityTag string) ([]DeviceEntry, error) {
	m, err := loadDeviceMapFunc(securityTag)
	if err != nil {
		return nil, fmt.Errorf("cannot open device map: %w", err)
	}
	defer m.Close()

	var entries []DeviceEntry

	err = m.Iterate(func(key ebpf.DeviceKey) error {
		entries = append(entries, DeviceEntry{
			DevType: key.Type,
			Major:   key.Major,
			Minor:   key.Minor,
			// The BPF device map only records whether access is
			// allowed (value=1), not individual r/w/m permissions.
			// Report "rwm" as presence in the map means full access.
			Access: "rwm",
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot iterate device map: %w", err)
	}

	return entries, nil
}

func collectDevicesV1(securityTag string) ([]DeviceEntry, error) {
	path := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/devices", securityTag, "devices.list")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer f.Close()

	var entries []DeviceEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// The data parsed here is coming directly from the kernel, limited
		// validation and gracefully skip on errors.
		err := func() error {
			// Format: "c 1:3 rwm" or "a *:* rwm"
			parts := strings.Fields(line)
			if l := len(parts); l != 3 {
				return fmt.Errorf("unexpected number of fields: %v", l)
			}

			devTypeStr := parts[0]
			majMin := parts[1]
			access := parts[2]

			majPart, minPart, found := strings.Cut(majMin, ":")
			if !found {
				return fmt.Errorf("unexpected format of major:minor")
			}

			major, err := strconv.ParseUint(majPart, 10, 32)
			if err != nil {
				if majPart == "*" {
					major = AccessAny
				} else {
					return fmt.Errorf("malformed major number: %w", err)
				}
			}

			minor, err := strconv.ParseUint(minPart, 10, 32)
			if err != nil {
				if minPart == "*" {
					minor = AccessAny
				} else {
					return fmt.Errorf("malformed minor number: %w", err)
				}
			}

			entries = append(entries, DeviceEntry{
				DevType: devTypeStr[0],
				Major:   uint32(major),
				Minor:   uint32(minor),
				Access:  access,
			})

			return nil
		}()
		if err != nil {
			logger.Debugf("cannot parse entry: %q: %v", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read devices.list: %w", err)
	}

	return entries, nil
}

// ErrDeviceCgroupV1Busy is returned when the device cgroup v1
// hierarchy of a snap cannot be removed because it is still in use,
// for instance when an application of the snap is still running.
// It is only ever returned when the cgroup v1 hierarchy is in use;
// removing the pinned BPF device map on cgroup v2 cannot fail with
// a busy-like error.
var ErrDeviceCgroupV1Busy = errors.New("device cgroup is still in use")

// RemoveDeviceCgroup removes the device cgroup artifacts of the given
// snap instance (v1 hierarchy directories or v2 pinned BPF maps).
// It returns nil if there is nothing to clean up (missing artifacts are
// not an error). If a v1 device cgroup is still in use, e.g. by a running
// snap application, the returned error matches ErrDeviceCgroupV1Busy.
func RemoveDeviceCgroup(instanceName naming.InstanceName) error {
	v, err := Version()
	if err != nil {
		return fmt.Errorf("cannot determine cgroup version: %w", err)
	}
	if v == V2 {
		return removeDeviceCgroupV2(instanceName)
	}
	return removeDeviceCgroupV1(instanceName)
}

func removeDeviceCgroupV2(instanceName naming.InstanceName) error {
	paths, err := findActiveDeviceMapPathsForSnapFunc(instanceName.String())
	if err != nil {
		return fmt.Errorf("cannot enumerate device maps for snap %q: %w", instanceName, err)
	}
	var firstErr error
	for _, path := range paths {
		// Attached programs keep the map alive via kernel refcounts,
		// so no busy handling is needed.
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("cannot remove %s: %w", path, err)
			}
		}
	}
	return firstErr
}

func removeDeviceCgroupV1(instanceName naming.InstanceName) error {
	// NOTE: unlike the v2 path, the hierarchy directories of component
	// hooks (snap.<instance>+<component>.hook.<hookname>) are not removed
	// here yet.
	cgroupDevicesPath := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/devices")
	prefix := fmt.Sprintf("snap.%s.", instanceName.String())
	entries, err := os.ReadDir(cgroupDevicesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot read %s: %w", cgroupDevicesPath, err)
	}
	var firstErr error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		path := filepath.Join(cgroupDevicesPath, e.Name())
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				// cgroupfs rmdir returns EBUSY when the cgroup still has
				// processes in it or has child cgroups
				if errors.Is(err, syscall.EBUSY) {
					firstErr = fmt.Errorf("%w: %v", ErrDeviceCgroupV1Busy, err)
				} else {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}
