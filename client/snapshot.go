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

// errors on reading:
var (
	ErrSnapshotUnsupported  = errors.New("unsupported snapshot format")
	ErrSnapshotSizeMismatch = errors.New("reported size and read size differ")
	ErrSnapshotHashMismatch = errors.New("hash mismatch")
)

// errors about not finding stuff (should bubble out as something more user-friendly, probably)
var (
	ErrSnapshotNotFound      = errors.New("no snapshot by that ID")
	ErrSnapshotSnapsNotFound = errors.New("the snapshot with the given ID does not contain the requested snaps")
)

// A SnapshotOp is used to request an operation on a snapshot
type SnapshotOp struct {
	ID     uint64   `json:"ID"`
	Action string   `json:"action"`
	Snaps  []string `json:"snaps,omitempty"`
	Homes  []string `json:"snaps,omitempty"`
}

func (op *SnapshotOp) String() string {
	// verb of snapshot #N [for snaps %q] [for homes %q]
	var snaps string
	var homes string
	if len(op.Snaps) > 0 {
		snaps = " for snaps " + strutil.Quoted(op.Snaps)
	}
	if len(op.Homes) > 0 {
		homes = " for homes " + strutil.Quoted(op.Homes)
	}
	return fmt.Sprintf("%s of snapshot #%d%s%s", strings.Title(op.Action), op.ID, snaps, homes)
}

// A Snapshot is a collection of archives with a simple metadata json file
// (and hashsums of everything)
//
// XXX can snapd sign these?
type Snapshot struct {
	// ID of the snapshot group (a snapshot group is a "snap save" invocation)
	ID uint64 `json:"snapshot"`
	// the snap this data is for
	Snap string `json:"snap"`
	// the snap's revision
	Revision snap.Revision `json:"revision"`
	// the snap's version (optional)
	Version string `json:"version,omitempty"`
	// the time this snapshot's data collection was started
	Time time.Time `json:"time"`
	// the hash of the data
	Hashsums map[string]string `json:"sha3-384"`
	// the system's config
	Config *json.RawMessage `json:"config,omitempty"`
	// the sum of the archive sizes
	Size int64 `json:"size,omitempty"`
}

// Uninitialised checks whether the snapshot is missing information that
// should be there for a snapshot that's just been opened.
func (sh *Snapshot) Uninitialised() bool {
	return sh == nil || sh.ID == 0 || sh.Snap == "" || sh.Revision.Unset() || len(sh.Hashsums) == 0 || sh.Time.IsZero()
}

// A SnapshotGroup is a set of Snapshots created by a single "snap save"
type SnapshotGroup struct {
	ID        uint64      `json:"id"`
	Snapshots []*Snapshot `json:"snapshots"`
}

// MinTime returns the earliest time in the group
func (sg SnapshotGroup) MinTime() time.Time {
	if len(sg.Snapshots) == 0 {
		return time.Time{}
	}
	mint := sg.Snapshots[0].Time
	for _, sh := range sg.Snapshots {
		if sh.Time.Before(mint) {
			mint = sh.Time
		}
	}
	return mint
}

func (client *Client) Snapshots(snapshotID uint64, snapNames []string) ([]SnapshotGroup, error) {
	q := make(url.Values)
	if snapshotID > 0 {
		q.Add("id", strconv.FormatUint(snapshotID, 10))
	}
	if len(snapNames) > 0 {
		q.Add("snaps", strings.Join(snapNames, ","))
	}

	var sgs []SnapshotGroup
	_, err := client.doSync("GET", "/v2/snapshots", q, nil, nil, &sgs)
	return sgs, err
}

func (client *Client) LoseSnapshot(snapshotID uint64, snaps []string) (changeID string, err error) {
	return client.snapshotOp(&SnapshotOp{
		ID:     snapshotID,
		Action: "lose",
		Snaps:  snaps,
	})
}

func (client *Client) CheckSnapshot(snapshotID uint64, snaps []string, homes []string) (changeID string, err error) {
	return client.snapshotOp(&SnapshotOp{
		ID:     snapshotID,
		Action: "check",
		Snaps:  snaps,
		Homes:  homes,
	})
}

func (client *Client) RestoreSnapshot(snapshotID uint64, snaps []string, homes []string) (changeID string, err error) {
	return client.snapshotOp(&SnapshotOp{
		ID:     snapshotID,
		Action: "restore",
		Snaps:  snaps,
		Homes:  homes,
	})
}

func (client *Client) snapshotOp(op *SnapshotOp) (changeID string, err error) {
	data, err := json.Marshal(op)
	if err != nil {
		return "", fmt.Errorf("cannot marshal snapshot action: %v", err)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/snapshots", nil, headers, bytes.NewBuffer(data))
}
