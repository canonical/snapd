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

package client_test

import (
	"crypto/sha256"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/snap"
)

func (cs *clientSuite) TestClientSnapshotIsValid(c *check.C) {
	now := time.Now()
	revno := snap.R(1)
	sums := map[string]string{"user/foo.tgz": "some long hash"}
	c.Check((&client.Snapshot{
		SetID:    42,
		Time:     now,
		Snap:     "asnap",
		Revision: revno,
		SHA3_384: sums,
	}).IsValid(), check.Equals, true)

	for desc, snapshot := range map[string]*client.Snapshot{
		"nil":     nil,
		"empty":   {},
		"no id":   { /*SetID: 42,*/ Time: now, Snap: "asnap", Revision: revno, SHA3_384: sums},
		"no time": {SetID: 42 /*Time: now,*/, Snap: "asnap", Revision: revno, SHA3_384: sums},
		"no snap": {SetID: 42, Time: now /*Snap: "asnap",*/, Revision: revno, SHA3_384: sums},
		"no rev":  {SetID: 42, Time: now, Snap: "asnap" /*Revision: revno,*/, SHA3_384: sums},
		"no sums": {SetID: 42, Time: now, Snap: "asnap", Revision: revno /*SHA3_384: sums*/},
	} {
		c.Check(snapshot.IsValid(), check.Equals, false, check.Commentf("%s", desc))
	}

}

func (cs *clientSuite) TestClientSnapshotSetTime(c *check.C) {
	// if set is empty, it doesn't explode (and returns the zero time)
	c.Check(client.SnapshotSet{}.Time().IsZero(), check.Equals, true)
	// if not empty, returns the earliest one
	c.Check(client.SnapshotSet{Snapshots: []*client.Snapshot{
		{Time: time.Unix(3, 0)},
		{Time: time.Unix(1, 0)},
		{Time: time.Unix(2, 0)},
	}}.Time(), check.DeepEquals, time.Unix(1, 0))
}

func (cs *clientSuite) TestClientSnapshotSetSize(c *check.C) {
	// if set is empty, doesn't explode (and returns 0)
	c.Check(client.SnapshotSet{}.Size(), check.Equals, int64(0))
	// if not empty, returns the sum
	c.Check(client.SnapshotSet{Snapshots: []*client.Snapshot{
		{Size: 1},
		{Size: 2},
		{Size: 3},
	}}.Size(), check.DeepEquals, int64(6))
}

func (cs *clientSuite) TestClientSnapshotSets(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{"id": 1}, {"id":2}]
}`
	sets, err := cs.cli.SnapshotSets(42, []string{"foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Check(sets, check.DeepEquals, []client.SnapshotSet{{ID: 1}, {ID: 2}})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snapshots")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"set":   []string{"42"},
		"snaps": []string{"foo,bar"},
	})
}

func (cs *clientSuite) testClientSnapshotActionFull(c *check.C, action string, users []string, f func() (string, error)) {
	cs.status = 202
	cs.rsp = `{
		"status-code": 202,
		"type": "async",
		"change": "1too3"
	}`
	id, err := f()
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "1too3")

	c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")

	act, err := client.UnmarshalSnapshotAction(cs.req.Body)
	c.Assert(err, check.IsNil)
	c.Check(act.SetID, check.Equals, uint64(42))
	c.Check(act.Action, check.Equals, action)
	c.Check(act.Snaps, check.DeepEquals, []string{"asnap", "bsnap"})
	c.Check(act.Users, check.DeepEquals, users)

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snapshots")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)
}

func (cs *clientSuite) TestClientForgetSnapshot(c *check.C) {
	cs.testClientSnapshotActionFull(c, "forget", nil, func() (string, error) {
		return cs.cli.ForgetSnapshots(42, []string{"asnap", "bsnap"})
	})
}

func (cs *clientSuite) testClientSnapshotAction(c *check.C, action string, f func(uint64, []string, []string) (string, error)) {
	cs.testClientSnapshotActionFull(c, action, []string{"auser", "buser"}, func() (string, error) {
		return f(42, []string{"asnap", "bsnap"}, []string{"auser", "buser"})
	})
}

func (cs *clientSuite) TestClientCheckSnapshots(c *check.C) {
	cs.testClientSnapshotAction(c, "check", cs.cli.CheckSnapshots)
}

func (cs *clientSuite) TestClientRestoreSnapshots(c *check.C) {
	cs.testClientSnapshotAction(c, "restore", cs.cli.RestoreSnapshots)
}

func (cs *clientSuite) TestClientExportSnapshotSpecificErr(c *check.C) {
	content := `{"type":"error","status-code":400,"result":{"message":"boom","kind":"err-kind","value":"err-value"}}`
	cs.contentLength = int64(len(content))
	cs.rsp = content
	cs.status = 400
	cs.header = http.Header{"Content-Type": []string{"application/json"}}
	_, _, err := cs.cli.SnapshotExport(42)
	c.Check(err, check.ErrorMatches, "boom")
}

func (cs *clientSuite) TestClientExportSnapshot(c *check.C) {
	type tableT struct {
		content     string
		contentType string
		status      int
	}

	table := []tableT{
		{"test-export", client.SnapshotExportMediaType, 200},
		{"test-export", "application/x-tar", 400},
		{"", "", 400},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %q", i, t.content)

		cs.contentLength = int64(len(t.content))
		cs.header = http.Header{"Content-Type": []string{t.contentType}}
		cs.rsp = t.content
		cs.status = t.status

		r, size, err := cs.cli.SnapshotExport(42)
		if t.status == 200 {
			c.Assert(err, check.IsNil, comm)
			c.Assert(cs.countingCloser.closeCalled, check.Equals, 0)
			c.Assert(size, check.Equals, int64(len(t.content)), comm)
		} else {
			c.Assert(err.Error(), check.Equals, "unexpected status code: ")
			c.Assert(cs.countingCloser.closeCalled, check.Equals, 1)
		}

		if t.status == 200 {
			buf, err := io.ReadAll(r)
			c.Assert(err, check.IsNil)
			c.Assert(string(buf), check.Equals, t.content)
		}
	}
}

func (cs *clientSuite) TestClientSnapshotImport(c *check.C) {
	type tableT struct {
		rsp    string
		status int
		setID  uint64
		error  string
	}
	table := []tableT{
		{`{"type": "sync", "result": {"set-id": 42, "snaps": ["baz", "bar", "foo"]}}`, 200, 42, ""},
		{`{"type": "error"}`, 400, 0, "server error: \"Bad Request\""},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %s", i, t.rsp)

		cs.rsp = t.rsp
		cs.status = t.status

		fakeSnapshotData := "fake"
		r := strings.NewReader(fakeSnapshotData)
		importSet, err := cs.cli.SnapshotImport(r, int64(len(fakeSnapshotData)))
		if t.error != "" {
			c.Assert(err, check.NotNil, comm)
			c.Check(err.Error(), check.Equals, t.error, comm)
			continue
		}
		c.Assert(err, check.IsNil, comm)
		c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, client.SnapshotExportMediaType)
		c.Assert(cs.req.Header.Get("Content-Length"), check.Equals, strconv.Itoa(len(fakeSnapshotData)))
		c.Check(importSet.ID, check.Equals, t.setID, comm)
		c.Check(importSet.Snaps, check.DeepEquals, []string{"baz", "bar", "foo"}, comm)
		d, err := io.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(d), check.Equals, fakeSnapshotData)
	}
}

func (cs *clientSuite) TestClientSnapshotContentHash(c *check.C) {
	now := time.Now()
	revno := snap.R(1)
	sums := map[string]string{"user/foo.tgz": "some long hash"}

	sh1 := &client.Snapshot{SetID: 1, Time: now, Snap: "asnap", Revision: revno, SHA3_384: sums}
	// sh1, sh1_1 are the same except time
	sh1_1 := &client.Snapshot{SetID: 1, Time: now.Add(10), Snap: "asnap", Revision: revno, SHA3_384: sums}
	// sh1, sh2 are the same except setID
	sh2 := &client.Snapshot{SetID: 2, Time: now, Snap: "asnap", Revision: revno, SHA3_384: sums}

	h1, err := sh1.ContentHash()
	c.Assert(err, check.IsNil)
	// content hash uses sha256 internally
	c.Check(h1, check.HasLen, sha256.Size)

	// same except time means same hash
	h1_1, err := sh1_1.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h1, check.DeepEquals, h1_1)

	// same except set means same hash
	h2, err := sh2.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h1, check.DeepEquals, h2)

	// sh3 is different because of snap name
	sh3 := &client.Snapshot{SetID: 1, Time: now, Snap: "other-snap", Revision: revno, SHA3_384: sums}
	h3, err := sh3.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h1, check.Not(check.DeepEquals), h3)

	// sh4 is different because of the sha3_384 sums
	sums4 := map[string]string{"user/foo.tgz": "some other hash"}
	sh4 := &client.Snapshot{SetID: 1, Time: now, Snap: "asnap", Revision: revno, SHA3_384: sums4}
	// same except sha3_384 means different hash
	h4, err := sh4.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h4, check.Not(check.DeepEquals), h1)

	// same except options means same hash
	sh5 := &client.Snapshot{SetID: 1, Time: now, Snap: "asnap", Revision: revno, SHA3_384: sums, Options: &snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/exclude"}}}
	h5, err := sh5.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h5, check.DeepEquals, h1)
}

func (cs *clientSuite) TestClientSnapshotSetContentHash(c *check.C) {
	sums := map[string]string{"user/foo.tgz": "some long hash"}
	ss1 := client.SnapshotSet{Snapshots: []*client.Snapshot{
		{SetID: 1, Snap: "snap2", Size: 2, SHA3_384: sums},
		{SetID: 1, Snap: "snap1", Size: 1, SHA3_384: sums},
		{SetID: 1, Snap: "snap3", Size: 3, SHA3_384: sums},
		{SetID: 1, Snap: "snap4", Size: 4, SHA3_384: sums},
	}}
	// ss2 is the same ss1 but in a different order with different setID, and in the last case
	// ss2 is the same as ss1 except for snapshot options
	ss2 := client.SnapshotSet{Snapshots: []*client.Snapshot{
		{SetID: 2, Snap: "snap3", Size: 3, SHA3_384: sums},
		{SetID: 2, Snap: "snap2", Size: 2, SHA3_384: sums},
		{SetID: 2, Snap: "snap1", Size: 1, SHA3_384: sums},
		{SetID: 2, Snap: "snap4", Size: 4, SHA3_384: sums, Options: &snap.SnapshotOptions{Exclude: []string{"$SNAP_DATA/exclude"}}},
	}}

	h1, err := ss1.ContentHash()
	c.Assert(err, check.IsNil)
	// content hash uses sha256 internally
	c.Check(h1, check.HasLen, sha256.Size)

	// h1 and h2 have the same hash
	h2, err := ss2.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h2, check.DeepEquals, h1)

	// ss3 is different because the size of snap3 is different
	ss3 := client.SnapshotSet{Snapshots: []*client.Snapshot{
		{SetID: 1, Snap: "snap2", Size: 2},
		{SetID: 1, Snap: "snap3", Size: 666666666},
		{SetID: 1, Snap: "snap1", Size: 1},
	}}
	// h1 and h3 are different
	h3, err := ss3.ContentHash()
	c.Assert(err, check.IsNil)
	c.Check(h3, check.Not(check.DeepEquals), h1)

}
