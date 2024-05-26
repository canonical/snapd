// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main_test

import (
	"fmt"
	"io"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

var newPreseedNewSnapdSameSysKey = `
{
    "result": {
        "preseed-start-time": "2020-07-24T21:41:33.838194712Z",
        "preseed-system-key": {
            "apparmor-features": [
                "caps",
                "dbus",
                "domain",
                "file",
                "mount",
                "namespaces",
                "network",
                "network_v8",
                "policy",
                "ptrace",
                "query",
                "rlimit",
                "signal"
            ],
            "apparmor-parser-features": [
                "unsafe"
            ],
            "apparmor-parser-mtime": 1589907589,
            "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
            "cgroup-version": "1",
            "nfs-home": false,
            "overlay-root": "",
            "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
            "seccomp-features": [
                "allow",
                "errno",
                "kill_process",
                "kill_thread",
                "log",
                "trace",
                "trap",
                "user_notif"
            ],
            "version": 10
        },
        "preseed-time": "2020-07-24T21:41:43.156401424Z",
        "preseeded": true,
        "seed-restart-system-key": {
            "apparmor-features": [
                "caps",
                "dbus",
                "domain",
                "file",
                "mount",
                "namespaces",
                "network",
                "network_v8",
                "policy",
                "ptrace",
                "query",
                "rlimit",
                "signal"
            ],
            "apparmor-parser-features": [
                "unsafe"
            ],
            "apparmor-parser-mtime": 1589907589,
            "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
            "cgroup-version": "1",
            "nfs-home": false,
            "overlay-root": "",
            "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
            "seccomp-features": [
                "allow",
                "errno",
                "kill_process",
                "kill_thread",
                "log",
                "trace",
                "trap",
                "user_notif"
            ],
            "version": 10
        },
        "seed-restart-time": "2020-07-24T21:42:16.646098923Z",
        "seed-start-time": "0001-01-01T00:00:00Z",
        "seed-time": "2020-07-24T21:42:20.518607Z",
        "seeded": true
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

var newPreseedNewSnapdDiffSysKey = `
{
    "result": {
        "preseed-start-time": "2020-07-24T21:41:33.838194712Z",
        "preseed-system-key": {
            "apparmor-features": [
                "caps",
                "dbus",
                "domain",
                "file",
                "mount",
                "namespaces",
                "network",
                "network_v8",
                "policy",
                "ptrace",
                "query",
                "rlimit",
                "signal"
            ],
            "apparmor-parser-features": [
                "unsafe"
            ],
            "apparmor-parser-mtime": 1589907589,
            "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
            "cgroup-version": "1",
            "nfs-home": false,
            "overlay-root": "",
            "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
            "seccomp-features": [
                "allow",
                "errno",
                "kill_process",
                "kill_thread",
                "log",
                "trace",
                "trap",
                "user_notif"
            ],
            "version": 10
        },
        "preseed-time": "2020-07-24T21:41:43.156401424Z",
        "preseeded": true,
        "seed-restart-system-key": {
            "apparmor-features": [
                "caps",
                "dbus",
                "domain",
                "file",
                "mount",
                "namespaces",
                "network",
                "policy",
                "ptrace",
                "query",
                "rlimit",
                "signal"
            ],
            "apparmor-parser-features": [
                "unsafe"
            ],
            "apparmor-parser-mtime": 1589907589,
            "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
            "cgroup-version": "1",
            "nfs-home": false,
            "overlay-root": "",
            "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
            "seccomp-features": [
                "allow",
                "errno",
                "kill",
                "log",
                "trace",
                "trap",
                "user_notif"
            ],
            "version": 10
        },
        "seed-restart-time": "2020-07-24T21:42:16.646098923Z",
        "seed-start-time": "0001-01-01T00:00:00Z",
        "seed-time": "2020-07-24T21:42:20.518607Z",
        "seeded": true
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

// a system that was not preseeded at all
var noPreseedingJSON = `
{
    "result": {
        "seed-time": "2019-07-04T19:16:10.548793375-05:00",
        "seeded": true
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

var seedingError = `{
    "result": {
        "preseed-start-time": "2020-07-24T21:41:33.838194712Z",
        "preseed-time": "2020-07-24T21:41:43.156401424Z",
        "preseeded": true,
        "seed-error": "cannot perform the following tasks:\n- xxx"
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

// a system that was preseeded, but didn't record the new keys
// this is the case for a system that was preseeded and then seeded with an old
// snapd, but then is refreshed to a version of snapd that supports snap debug
// seeding, where we want to still have sensible output
var oldPreseedingJSON = `{
    "result": {
        "preseed-start-time": "0001-01-01T00:00:00Z",
        "preseed-time": "0001-01-01T00:00:00Z",
        "seed-restart-time": "2019-07-04T19:14:10.548793375-05:00",
        "seed-start-time": "0001-01-01T00:00:00Z",
        "seed-time": "2019-07-04T19:16:10.548793375-05:00",
        "seeded": true,
        "preseeded": true
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

var stillSeeding = `{
    "result": {
        "preseed-start-time": "2020-07-24T21:41:33.838194712Z",
        "preseed-time": "2020-07-24T21:41:43.156401424Z",
        "preseeded": true
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

var stillSeedingNoPreseed = `{
    "result": {},
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`

func (s *SnapSuite) TestDebugSeeding(c *C) {
	tt := []struct {
		jsonResp   string
		expStdout  string
		expStderr  string
		expErr     string
		comment    string
		hasUnicode bool
	}{
		{
			jsonResp: newPreseedNewSnapdSameSysKey,
			expStdout: `
seeded:            true
preseeded:         true
image-preseeding:  9.318s
seed-completion:   3.873s
`[1:],
			comment: "new preseed keys, same system-key",
		},
		{
			jsonResp: newPreseedNewSnapdDiffSysKey,
			expStdout: `
seeded:            true
preseeded:         true
image-preseeding:  9.318s
seed-completion:   3.873s
preseed-system-key: {
  "apparmor-features": [
    "caps",
    "dbus",
    "domain",
    "file",
    "mount",
    "namespaces",
    "network",
    "network_v8",
    "policy",
    "ptrace",
    "query",
    "rlimit",
    "signal"
  ],
  "apparmor-parser-features": [
    "unsafe"
  ],
  "apparmor-parser-mtime": 1589907589,
  "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
  "cgroup-version": "1",
  "nfs-home": false,
  "overlay-root": "",
  "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
  "seccomp-features": [
    "allow",
    "errno",
    "kill_process",
    "kill_thread",
    "log",
    "trace",
    "trap",
    "user_notif"
  ],
  "version": 10
}
seed-restart-system-key: {
  "apparmor-features": [
    "caps",
    "dbus",
    "domain",
    "file",
    "mount",
    "namespaces",
    "network",
    "policy",
    "ptrace",
    "query",
    "rlimit",
    "signal"
  ],
  "apparmor-parser-features": [
    "unsafe"
  ],
  "apparmor-parser-mtime": 1589907589,
  "build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
  "cgroup-version": "1",
  "nfs-home": false,
  "overlay-root": "",
  "seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
  "seccomp-features": [
    "allow",
    "errno",
    "kill",
    "log",
    "trace",
    "trap",
    "user_notif"
  ],
  "version": 10
}
`[1:],
			comment: "new preseed keys, different system-key",
		},
		{
			jsonResp: noPreseedingJSON,
			expStdout: `
seeded:           true
preseeded:        false
seed-completion:  --
`[1:],
			comment: "not preseeded no unicode",
		},
		{
			jsonResp: noPreseedingJSON,
			expStdout: `
seeded:           true
preseeded:        false
seed-completion:  –
`[1:],
			comment:    "not preseeded",
			hasUnicode: true,
		},
		{
			jsonResp: oldPreseedingJSON,
			expStdout: `
seeded:            true
preseeded:         true
image-preseeding:  0s
seed-completion:   2m0s
`[1:],
			comment: "old preseeded json",
		},
		{
			jsonResp: stillSeeding,
			expStdout: `
seeded:            false
preseeded:         true
image-preseeding:  9.318s
seed-completion:   --
`[1:],
			comment: "preseeded, still seeding no unicode",
		},
		{
			jsonResp: stillSeeding,
			expStdout: `
seeded:            false
preseeded:         true
image-preseeding:  9.318s
seed-completion:   –
`[1:],
			hasUnicode: true,
			comment:    "preseeded, still seeding",
		},
		{
			jsonResp: stillSeedingNoPreseed,
			expStdout: `
seeded:           false
preseeded:        false
seed-completion:  --
`[1:],
			comment: "not preseeded, still seeding no unicode",
		},
		{
			jsonResp: stillSeedingNoPreseed,
			expStdout: `
seeded:           false
preseeded:        false
seed-completion:  –
`[1:],
			hasUnicode: true,
			comment:    "not preseeded, still seeding",
		},
		{
			jsonResp: seedingError,
			expStdout: `
seeded:  false
seed-error: |
  cannot perform the following tasks:
  - xxx
preseeded:         true
image-preseeding:  9.318s
seed-completion:   --
`[1:],
			comment: "preseeded, error during seeding",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		n := 0
		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			n++
			switch n {
			case 1:
				c.Assert(r.Method, Equals, "GET", comment)
				c.Assert(r.URL.Path, Equals, "/v2/debug", comment)
				c.Assert(r.URL.RawQuery, Equals, "aspect=seeding", comment)
				data := mylog.Check2(io.ReadAll(r.Body))
				c.Assert(err, IsNil, comment)
				c.Assert(string(data), Equals, "", comment)
				fmt.Fprintln(w, t.jsonResp)
			default:
				c.Fatalf("expected to get 1 request, now on %d", n)
			}
		})
		args := []string{"debug", "seeding"}
		if t.hasUnicode {
			args = append(args, "--unicode=always")
		}
		rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs(args))
		if t.expErr != "" {
			c.Assert(err, ErrorMatches, t.expErr, comment)
			c.Assert(s.Stdout(), Equals, "", comment)
			c.Assert(s.Stderr(), Equals, t.expStderr, comment)
			continue
		}
		c.Assert(err, IsNil, comment)
		c.Assert(rest, DeepEquals, []string{}, comment)
		c.Assert(s.Stdout(), Equals, t.expStdout, comment)
		c.Assert(s.Stderr(), Equals, "", comment)
		c.Assert(n, Equals, 1, comment)

		s.ResetStdStreams()
	}
}
