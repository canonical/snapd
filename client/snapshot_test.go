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
	"net/url"
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
