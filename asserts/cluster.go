// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package asserts

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// Cluster holds a cluster assertion, which describes a cluster of devices and
// their organization into subclusters.
type Cluster struct {
	assertionBase
	seq         int
	devices     []ClusterDevice
	subclusters []Subcluster
}

// ClusterDevice holds the details about a device in a cluster assertion.
type ClusterDevice struct {
	// ID is the unique identifier referenced from subclusters.
	ID int
	// Addresses contains this device's known IP addresses.
	Addresses []string
	// DeviceID contains a unique tuple of identifiers for this device.
	DeviceID
}

// Subcluster holds the details about a subcluster in a cluster
// assertion.
type Subcluster struct {
	// Name is the subcluster's name.
	Name string
	// Devices lists device IDs that belong to this subcluster.
	Devices []int
	// Snaps contains the expected snap state for this subcluster.
	Snaps []ClusterSnap
}

// ClusterSnapState describes the relationship of a snap to the cluster.
type ClusterSnapState string

const (
	ClusterSnapStateClustered ClusterSnapState = "clustered"
	ClusterSnapStateEvacuated ClusterSnapState = "evacuated"
	ClusterSnapStateRemoved   ClusterSnapState = "removed"
)

// ClusterSnap holds the details about a snap in a subcluster.
type ClusterSnap struct {
	// State describes the snap's state in the cluster (clustered, evacuated,
	// removed).
	State ClusterSnapState
	// Instance is the snap's instance name.
	Instance string
	// Channel is the channel the snap should track.
	Channel string
}

func validateClusterSnapState(state string) error {
	switch ClusterSnapState(state) {
	case ClusterSnapStateClustered, ClusterSnapStateEvacuated, ClusterSnapStateRemoved:
		return nil
	default:
		return fmt.Errorf("snap state must be one of: %s", strutil.Quoted([]string{
			string(ClusterSnapStateClustered), string(ClusterSnapStateEvacuated), string(ClusterSnapStateRemoved),
		}))
	}
}

// ClusterID returns the cluster's ID.
func (c *Cluster) ClusterID() string {
	return c.HeaderString("cluster-id")
}

// Sequence returns the sequence number of this cluster assertion.
func (c *Cluster) Sequence() int {
	return c.seq
}

// Devices returns the list of devices in the cluster.
func (c *Cluster) Devices() []ClusterDevice {
	return c.devices
}

// Subclusters returns the list of subclusters.
func (c *Cluster) Subclusters() []Subcluster {
	return c.subclusters
}

func checkClusterDevice(device map[string]any) (ClusterDevice, error) {
	id, err := checkInt(device, "id")
	if err != nil {
		return ClusterDevice{}, err
	}

	if id <= 0 {
		return ClusterDevice{}, fmt.Errorf(`"id" header must be >=1: %d`, id)
	}

	dev, err := checkNotEmptyString(device, "device")
	if err != nil {
		return ClusterDevice{}, err
	}

	deviceID, err := newDeviceIDFromString(dev)
	if err != nil {
		return ClusterDevice{}, err
	}

	addresses, err := checkStringList(device, "addresses")
	if err != nil {
		return ClusterDevice{}, err
	}

	return ClusterDevice{
		ID:        id,
		Addresses: addresses,
		DeviceID:  deviceID,
	}, nil
}

func checkClusterDevices(devices []any) ([]ClusterDevice, error) {
	result := make([]ClusterDevice, 0, len(devices))
	seenIDs := make(map[int]bool, len(devices))
	for _, entry := range devices {
		device, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New(`"devices" field must be a list of maps`)
		}

		d, err := checkClusterDevice(device)
		if err != nil {
			return nil, err
		}

		if seenIDs[d.ID] {
			return nil, fmt.Errorf(`"devices" field contains duplicate device id %d`, d.ID)
		}
		seenIDs[d.ID] = true

		result = append(result, d)
	}
	return result, nil
}

func checkClusterSnap(snap map[string]any) (ClusterSnap, error) {
	state, err := checkNotEmptyString(snap, "state")
	if err != nil {
		return ClusterSnap{}, err
	}

	if err := validateClusterSnapState(state); err != nil {
		return ClusterSnap{}, err
	}

	instance, err := checkNotEmptyString(snap, "instance")
	if err != nil {
		return ClusterSnap{}, err
	}

	if err := naming.ValidateInstance(instance); err != nil {
		return ClusterSnap{}, fmt.Errorf("invalid snap instance name: %v", err)
	}

	ch, err := checkNotEmptyString(snap, "channel")
	if err != nil {
		return ClusterSnap{}, err
	}

	if _, err := channel.Parse(ch, ""); err != nil {
		return ClusterSnap{}, fmt.Errorf("invalid channel name %q: %v", ch, err)
	}

	return ClusterSnap{
		State:    ClusterSnapState(state),
		Instance: instance,
		Channel:  ch,
	}, nil
}

func checkClusterSnaps(snaps []any) ([]ClusterSnap, error) {
	result := make([]ClusterSnap, 0, len(snaps))
	for _, entry := range snaps {
		snap, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New(`"snaps" field must be a list of maps`)
		}

		s, err := checkClusterSnap(snap)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}

	return result, nil
}

func checkClusterSubcluster(subcluster map[string]any) (Subcluster, error) {
	name, err := checkNotEmptyString(subcluster, "name")
	if err != nil {
		return Subcluster{}, err
	}

	devices, err := checkStringList(subcluster, "devices")
	if err != nil {
		return Subcluster{}, err
	}

	ids := make([]int, 0, len(devices))
	for _, dev := range devices {
		id, err := atoi(dev, "device id %q", dev)
		if err != nil {
			return Subcluster{}, err
		}
		if id <= 0 {
			return Subcluster{}, fmt.Errorf("device id must be >=1: %d", id)
		}
		ids = append(ids, id)
	}

	list, err := checkList(subcluster, "snaps")
	if err != nil {
		return Subcluster{}, err
	}

	snaps, err := checkClusterSnaps(list)
	if err != nil {
		return Subcluster{}, err
	}

	return Subcluster{
		Name:    name,
		Devices: ids,
		Snaps:   snaps,
	}, nil
}

func checkClusterSubclusters(subclusters []any) ([]Subcluster, error) {
	result := make([]Subcluster, 0, len(subclusters))
	names := make(map[string]bool, len(subclusters))
	for _, entry := range subclusters {
		subcluster, ok := entry.(map[string]any)
		if !ok {
			return nil, errors.New(`"subclusters" field must be a list of maps`)
		}

		s, err := checkClusterSubcluster(subcluster)
		if err != nil {
			return nil, err
		}

		if names[s.Name] {
			return nil, fmt.Errorf(`"subclusters" field contains duplicate subcluster name %q`, s.Name)
		}
		names[s.Name] = true

		result = append(result, s)
	}

	return result, nil
}

func validateClusterDeviceIDs(devices []ClusterDevice, subclusters []Subcluster) error {
	seen := make(map[int]bool, len(devices))
	for _, device := range devices {
		seen[device.ID] = true
	}

	for _, subcluster := range subclusters {
		for _, id := range subcluster.Devices {
			if !seen[id] {
				return fmt.Errorf("\"subclusters\" references unknown device id %d", id)
			}
		}
	}

	return nil
}

func assembleCluster(assert assertionBase) (Assertion, error) {
	seq, err := checkSequence(assert.headers, "sequence")
	if err != nil {
		return nil, err
	}

	list, err := checkList(assert.headers, "devices")
	if err != nil {
		return nil, err
	}

	devices, err := checkClusterDevices(list)
	if err != nil {
		return nil, err
	}

	list, err = checkList(assert.headers, "subclusters")
	if err != nil {
		return nil, err
	}

	subclusters, err := checkClusterSubclusters(list)
	if err != nil {
		return nil, err
	}

	if err := validateClusterDeviceIDs(devices, subclusters); err != nil {
		return nil, err
	}

	return &Cluster{
		assertionBase: assert,
		seq:           seq,
		devices:       devices,
		subclusters:   subclusters,
	}, nil
}
