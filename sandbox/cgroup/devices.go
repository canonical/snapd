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
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/ebpf"
)

// deviceMapAccessor abstracts iteration over device map entries.
type deviceMapAccessor interface {
	Iterate(fn func(ebpf.DeviceKey) error) error
	Close() error
}

var (
	loadDeviceMapFunc         = loadDeviceMapImpl
	findDeviceMapsForSnapFunc = ebpf.FindActiveDeviceMapsForSnap
)

func loadDeviceMapImpl(securityTag string) (deviceMapAccessor, error) {
	return ebpf.LoadDeviceMap(securityTag)
}

// FindActiveDeviceMediationForSnap finds all security tags for a given
// snap name which have active device mediation.
func FindActiveDeviceMediationForSnap(snapName string) (tags []string, err error) {
	if probeErr != nil {
		return nil, fmt.Errorf("cannot determine cgroup version: %v", probeErr)
	}
	if probeVersion == V2 {
		return findDeviceMapsForSnapFunc(snapName)
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
		return nil, fmt.Errorf("cannot read %s: %v", cgroupDevicesPath, err)
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
	if probeErr != nil {
		return nil, fmt.Errorf("cannot determine cgroup version: %v", probeErr)
	}
	if probeVersion == V2 {
		return collectDevicesV2(tag)
	}
	return collectDevicesV1(tag)
}

func collectDevicesV2(securityTag string) ([]DeviceEntry, error) {
	m, err := loadDeviceMapFunc(securityTag)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("cannot iterate device map: %v", err)
	}

	return entries, nil
}

func collectDevicesV1(securityTag string) ([]DeviceEntry, error) {
	path := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/devices", securityTag, "devices.list")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %v", path, err)
	}
	defer f.Close()

	var entries []DeviceEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "c 1:3 rwm" or "a *:* rwm"
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		devTypeStr := parts[0]
		majMin := parts[1]
		access := parts[2]

		majMinParts := strings.SplitN(majMin, ":", 2)
		if len(majMinParts) != 2 {
			continue
		}
		major, err := strconv.ParseUint(majMinParts[0], 10, 32)
		if err != nil {
			if majMinParts[0] == "*" {
				major = AccessAny
			} else {
				return nil, fmt.Errorf("cannot parse major number: %v", err)
			}
		}

		minor, err := strconv.ParseUint(majMinParts[1], 10, 32)
		if err != nil {
			if majMinParts[1] == "*" {
				minor = AccessAny
			} else {
				return nil, fmt.Errorf("cannot parse minor number: %v", err)
			}
		}

		entries = append(entries, DeviceEntry{
			DevType: devTypeStr[0],
			Major:   uint32(major),
			Minor:   uint32(minor),
			Access:  access,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
