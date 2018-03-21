// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var (
	ErrSnapshotNotFound      = errors.New("no snapshot by that ID")
	ErrSnapshotSnapsNotFound = errors.New("the snapshot with the given ID does not contain the requested snaps")
)

// A SnapshotAction is used to request an operation on a snapshot
type SnapshotAction struct {
	ID     uint64   `json:"id"`
	Action string   `json:"action"`
	Snaps  []string `json:"snaps,omitempty"`
	Users  []string `json:"snaps,omitempty"`
}

func (action *SnapshotAction) String() string {
	// verb of snapshot #N [for snaps %q] [for users %q]
	var snaps string
	var users string
	if len(action.Snaps) > 0 {
		snaps = " for snaps " + strutil.Quoted(action.Snaps)
	}
	if len(action.Users) > 0 {
		users = " for users " + strutil.Quoted(action.Users)
	}
	return fmt.Sprintf("%s of snapshot #%d%s%s", strings.Title(action.Action), action.ID, snaps, users)
}

// A Snapshot is a collection of archives with a simple metadata json file
// (and hashsums of everything)
type Snapshot struct {
	// SetID is the ID of the snapshot set (a snapshot set is a "snap save" invocation)
	SetID uint64 `json:"snapshot"`
	// the snap this data is for
	Snap string `json:"snap"`
	// the snap's revision
	Revision snap.Revision `json:"revision"`
	// the snap's version
	Version string `json:"version"`
	// the time this snapshot's data collection was started
	Time time.Time `json:"time"`

	// the hash of the archives' data, keyed by archive path
	// (either 'archive.tgz' for the system archive, or
	// user/<username>.tgz for each user)
	SHA3_384 map[string]string `json:"sha3-384"`
	// the system's config
	Config *json.RawMessage `json:"config,omitempty"`
	// the sum of the archive sizes
	Size int64 `json:"size,omitempty"`

	// TBD: look int having snapd sign these
}

// IsValid checks whether the snapshot is missing information that
// should be there for a snapshot that's just been opened.
func (sh *Snapshot) IsValid() bool {
	return sh == nil || sh.SetID == 0 || sh.Snap == "" || sh.Revision.Unset() || len(sh.SHA3_384) == 0 || sh.Time.IsZero()
}

// A SnapshotSet is a set of Snapshots created by a single "snap save"
type SnapshotSet struct {
	ID        uint64      `json:"id"`
	Snapshots []*Snapshot `json:"snapshots"`
}

// Time returns the earliest time in the set
func (ss SnapshotSet) Time() time.Time {
	if len(ss.Snapshots) == 0 {
		return time.Time{}
	}
	mint := ss.Snapshots[0].Time
	for _, sh := range ss.Snapshots {
		if sh.Time.Before(mint) {
			mint = sh.Time
		}
	}
	return mint
}

// Size returns the sum of the set's sizes
func (ss SnapshotSet) Size() int64 {
	var sum int64
	for _, sh := range ss.Snapshots {
		sum += sh.Size
	}
	return sum
}

// Snapshots lists the snapshots in the system that match the given id (if
// non-zero) and snap names (if non-empty).
func (client *Client) Snapshots(setID uint64, snapNames []string) ([]SnapshotSet, error) {
	q := make(url.Values)
	if setID > 0 {
		q.Add("id", strconv.FormatUint(setID, 10))
	}
	if len(snapNames) > 0 {
		q.Add("snaps", strings.Join(snapNames, ","))
	}

	var snapshotSets []SnapshotSet
	_, err := client.doSync("GET", "/v2/snapshots", q, nil, nil, &snapshotSets)
	return snapshotSets, err
}

// ForgetSnapshot permanently removes the snapshot of the given id, limited to
// the given snaps (if non-empty).
func (client *Client) ForgetSnapshot(setID uint64, snaps []string) (changeID string, err error) {
	return client.snapshotAction(&SnapshotAction{
		ID:     setID,
		Action: "forget",
		Snaps:  snaps,
	})
}

// CheckSnapshot verifies the archive checksums in the given snapshot.
//
// If snaps or users are non-empty, limit to checking only those
// archives of the snapshot.
func (client *Client) CheckSnapshot(setID uint64, snaps []string, users []string) (changeID string, err error) {
	return client.snapshotAction(&SnapshotAction{
		ID:     setID,
		Action: "check",
		Snaps:  snaps,
		Users:  users,
	})
}

// RestoreSnapshot extracts the given snapshot.
//
// If snaps or users are non-empty, limit to checking only those
// archives of the snapshot.
func (client *Client) RestoreSnapshot(setID uint64, snaps []string, users []string) (changeID string, err error) {
	return client.snapshotAction(&SnapshotAction{
		ID:     setID,
		Action: "restore",
		Snaps:  snaps,
		Users:  users,
	})
}

func (client *Client) snapshotAction(action *SnapshotAction) (changeID string, err error) {
	data, err := json.Marshal(action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal snapshot action: %v", err)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/snapshots", nil, headers, bytes.NewBuffer(data))
}
