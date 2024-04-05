// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/testutil"
)

var chanName = "achan"

var ops = []struct {
	op     func(*client.Client, string, *client.SnapOptions) (string, error)
	action string
}{
	{(*client.Client).Install, "install"},
	{(*client.Client).Refresh, "refresh"},
	{(*client.Client).Remove, "remove"},
	{(*client.Client).Revert, "revert"},
	{(*client.Client).Enable, "enable"},
	{(*client.Client).Disable, "disable"},
	{(*client.Client).Switch, "switch"},
	{(*client.Client).HoldRefreshes, "hold"},
	{(*client.Client).UnholdRefreshes, "unhold"},
}

var multiOps = []struct {
	op     func(*client.Client, []string, *client.SnapOptions) (string, error)
	action string
}{
	{(*client.Client).RefreshMany, "refresh"},
	{(*client.Client).InstallMany, "install"},
	{(*client.Client).RemoveMany, "remove"},
	{(*client.Client).HoldRefreshesMany, "hold"},
	{(*client.Client).UnholdRefreshesMany, "unhold"},
}

func (cs *clientSuite) TestClientOpSnapServerError(c *check.C) {
	cs.err = errors.New("fail")
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName, nil)
		c.Check(err, check.ErrorMatches, `.*fail`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiOpSnapServerError(c *check.C) {
	cs.err = errors.New("fail")
	for _, s := range multiOps {
		_, err := s.op(cs.cli, nil, nil)
		c.Check(err, check.ErrorMatches, `.*fail`, check.Commentf(s.action))
	}
	_, _, err := cs.cli.SnapshotMany(nil, nil)
	c.Check(err, check.ErrorMatches, `.*fail`)
}

func (cs *clientSuite) TestClientOpSnapResponseError(c *check.C) {
	cs.status = 400
	cs.rsp = `{"type": "error"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName, nil)
		c.Check(err, check.ErrorMatches, `.*server error: "Bad Request"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiOpSnapResponseError(c *check.C) {
	cs.status = 500
	cs.rsp = `{"type": "error"}`
	for _, s := range multiOps {
		_, err := s.op(cs.cli, nil, nil)
		c.Check(err, check.ErrorMatches, `.*server error: "Internal Server Error"`, check.Commentf(s.action))
	}
	_, _, err := cs.cli.SnapshotMany(nil, nil)
	c.Check(err, check.ErrorMatches, `.*server error: "Internal Server Error"`)
}

func (cs *clientSuite) TestClientOpSnapBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName, nil)
		c.Check(err, check.ErrorMatches, `.*expected async response for "POST" on "/v2/snaps/`+pkgName+`", got "what"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNotAccepted(c *check.C) {
	cs.rsp = `{
		"status-code": 200,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName, nil)
		c.Check(err, check.ErrorMatches, `.*operation not accepted`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNoChange(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName, nil)
		c.Assert(err, check.ErrorMatches, `.*response without change reference.*`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnap(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		id, err := s.op(cs.cli, pkgName, &client.SnapOptions{Channel: chanName})
		c.Assert(err, check.IsNil)

		c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json", check.Commentf(s.action))

		_, ok := cs.req.Context().Deadline()
		c.Check(ok, check.Equals, true)

		body, err := io.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]string)
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody["channel"], check.Equals, chanName, check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 2, check.Commentf(s.action))

		c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName), check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiOpSnap(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range multiOps {
		// Note body is essentially the same as TestClientMultiSnapshot; keep in sync
		id, err := s.op(cs.cli, []string{pkgName}, nil)
		c.Assert(err, check.IsNil)

		c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json", check.Commentf(s.action))

		body, err := io.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]interface{})
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody["snaps"], check.DeepEquals, []interface{}{pkgName}, check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 2, check.Commentf(s.action))

		c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps", check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiOpSnapTransactional(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range multiOps {
		// Note body is essentially the same as TestClientMultiSnapshot; keep in sync
		id, err := s.op(cs.cli, []string{pkgName},
			&client.SnapOptions{Transaction: client.TransactionAllSnaps})
		c.Assert(err, check.IsNil)

		c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json", check.Commentf(s.action))

		body, err := io.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]interface{})
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody["snaps"], check.DeepEquals, []interface{}{pkgName}, check.Commentf(s.action))
		c.Check(jsonBody["transaction"], check.Equals, string(client.TransactionAllSnaps),
			check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 3, check.Commentf(s.action))

		c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps", check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiOpSnapIgnoreRunning(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range multiOps {
		// Note body is essentially the same as TestClientMultiSnapshot; keep in sync
		id, err := s.op(cs.cli, []string{pkgName},
			&client.SnapOptions{IgnoreRunning: true})
		c.Assert(err, check.IsNil)

		c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json", check.Commentf(s.action))

		body, err := io.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]interface{})
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody["snaps"], check.DeepEquals, []interface{}{pkgName}, check.Commentf(s.action))
		c.Check(jsonBody["ignore-running"], check.Equals, true,
			check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 3, check.Commentf(s.action))

		c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps", check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientMultiSnapshot(c *check.C) {
	// Note body is essentially the same as TestClientMultiOpSnap; keep in sync
	cs.status = 202
	cs.rsp = `{
                "result": {"set-id": 42},
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	setID, changeID, err := cs.cli.SnapshotMany([]string{pkgName}, nil)
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	jsonBody := make(map[string]interface{})
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Check(jsonBody["action"], check.Equals, "snapshot")
	c.Check(jsonBody["snaps"], check.DeepEquals, []interface{}{pkgName})
	c.Check(jsonBody, check.HasLen, 2)
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Check(setID, check.Equals, uint64(42))
	c.Check(changeID, check.Equals, "d728")
}

func (cs *clientSuite) TestClientOpInstallPath(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	id, err := cs.cli.InstallPath(snap, "", nil)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), testutil.Contains, "\r\nsnap-data\r\n")
	c.Assert(string(body), testutil.Contains, "Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n")

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Assert(cs.req.Header.Get("Content-Type"), testutil.Contains, "multipart/form-data; boundary=")
	_, ok := cs.req.Context().Deadline()
	c.Assert(ok, check.Equals, false)
	c.Check(id, check.Equals, "66b3")
}

func (cs *clientSuite) TestClientOpInstallPathIgnoreRunning(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	id, err := cs.cli.InstallPath(snap, "", &client.SnapOptions{IgnoreRunning: true})
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"ignore-running\"\r\n\r\ntrue\r\n.*")

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Assert(cs.req.Header.Get("Content-Type"), check.Matches, "multipart/form-data; boundary=.*")
	_, ok := cs.req.Context().Deadline()
	c.Assert(ok, check.Equals, false)
	c.Check(id, check.Equals, "66b3")
}

func (cs *clientSuite) TestClientOpInstallPathInstance(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	id, err := cs.cli.InstallPath(snap, "foo_bar", nil)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches, "(?s).*\r\nsnap-data\r\n.*")
	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"action\"\r\n\r\ninstall\r\n.*")
	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"name\"\r\n\r\nfoo_bar\r\n.*")

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Assert(cs.req.Header.Get("Content-Type"), check.Matches, "multipart/form-data; boundary=.*")
	c.Check(id, check.Equals, "66b3")
}

func (cs *clientSuite) TestClientOpInstallPathMany(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`

	var paths []string
	names := []string{"foo.snap", "bar.snap"}
	for _, name := range names {
		path := filepath.Join(c.MkDir(), name)
		paths = append(paths, path)
		c.Assert(os.WriteFile(path, []byte("snap-data"), 0644), check.IsNil)
	}

	id, err := cs.cli.InstallPathMany(paths, nil)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	for _, name := range names {
		c.Assert(string(body), check.Matches, fmt.Sprintf(`(?s).*Content-Disposition: form-data; name="snap"; filename="%s"\r\nContent-Type: application/octet-stream\r\n\r\nsnap-data\r\n.*`, name))

	}
	c.Assert(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="action"\r\n\r\ninstall\r\n.*`)

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Assert(cs.req.Header.Get("Content-Type"), check.Matches, "multipart/form-data; boundary=.*")

	_, ok := cs.req.Context().Deadline()
	c.Assert(ok, check.Equals, false)
	c.Check(id, check.Equals, "66b3")
}

func (cs *clientSuite) TestClientOpInstallPathManyTransactionally(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`

	var paths []string
	names := []string{"foo.snap", "bar.snap"}
	for _, name := range names {
		path := filepath.Join(c.MkDir(), name)
		paths = append(paths, path)
		c.Assert(os.WriteFile(path, []byte("snap-data"), 0644), check.IsNil)
	}

	id, err := cs.cli.InstallPathMany(paths, &client.SnapOptions{Transaction: client.TransactionAllSnaps})
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	for _, name := range names {
		c.Assert(string(body), check.Matches, fmt.Sprintf(`(?s).*Content-Disposition: form-data; name="snap"; filename="%s"\r\nContent-Type: application/octet-stream\r\n\r\nsnap-data\r\n.*`, name))

	}
	c.Assert(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="action"\r\n\r\ninstall\r\n.*`)
	c.Assert(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="transaction"\r\n\r\nall-snaps\r\n.*`)

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Assert(cs.req.Header.Get("Content-Type"), check.Matches, "multipart/form-data; boundary=.*")

	_, ok := cs.req.Context().Deadline()
	c.Assert(ok, check.Equals, false)
	c.Check(id, check.Equals, "66b3")
}

func (cs *clientSuite) TestClientOpInstallPathManyWithOptions(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`

	var paths []string
	for _, name := range []string{"foo.snap", "bar.snap"} {
		path := filepath.Join(c.MkDir(), name)
		paths = append(paths, path)
		c.Assert(os.WriteFile(path, []byte("snap-data"), 0644), check.IsNil)
	}

	// InstallPathMany supports opts
	_, err := cs.cli.InstallPathMany(paths, &client.SnapOptions{
		Dangerous: true,
		DevMode:   true,
		Classic:   true,
	})

	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Check(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="dangerous"\r\n\r\ntrue\r\n.*`)
	c.Check(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="devmode"\r\n\r\ntrue\r\n.*`)
	c.Check(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="classic"\r\n\r\ntrue\r\n.*`)
}

func (cs *clientSuite) TestClientOpInstallPathManyWithQuotaGroup(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`

	var paths []string
	for _, name := range []string{"foo.snap", "bar.snap"} {
		path := filepath.Join(c.MkDir(), name)
		paths = append(paths, path)
		c.Assert(os.WriteFile(path, []byte("snap-data"), 0644), check.IsNil)
	}

	// Verify that the quota group option is serialized as a part of multipart form.
	_, err := cs.cli.InstallPathMany(paths, &client.SnapOptions{
		Dangerous:      true,
		QuotaGroupName: "foo-group",
	})

	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Check(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="dangerous"\r\n\r\ntrue\r\n.*`)
	c.Check(string(body), check.Matches, `(?s).*Content-Disposition: form-data; name="quota-group"\r\n\r\nfoo-group\r\n.*`)
}

func (cs *clientSuite) TestClientOpInstallDangerous(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	opts := client.SnapOptions{
		Dangerous: true,
	}

	// InstallPath takes Dangerous
	_, err = cs.cli.InstallPath(snap, "", &opts)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"dangerous\"\r\n\r\ntrue\r\n.*")

	// Install does not (and gives us a clear error message)
	_, err = cs.cli.Install("foo", &opts)
	c.Assert(err, check.Equals, client.ErrDangerousNotApplicable)

	// InstallMany just ignores it without error for the moment
	_, err = cs.cli.InstallMany([]string{"foo"}, &opts)
	c.Assert(err, check.IsNil)
}

func (cs *clientSuite) TestClientOpInstallUnaliased(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	opts := client.SnapOptions{
		Unaliased: true,
	}

	_, err = cs.cli.Install("foo", &opts)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	jsonBody := make(map[string]interface{})
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil, check.Commentf("body: %v", string(body)))
	c.Check(jsonBody["unaliased"], check.Equals, true, check.Commentf("body: %v", string(body)))

	_, err = cs.cli.InstallPath(snap, "", &opts)
	c.Assert(err, check.IsNil)

	body, err = io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"unaliased\"\r\n\r\ntrue\r\n.*")
}

func (cs *clientSuite) TestClientOpInstallTransactional(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	opts := client.SnapOptions{
		Transaction: client.TransactionAllSnaps,
	}

	_, err = cs.cli.InstallMany([]string{"foo", "bar"}, &opts)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	jsonBody := make(map[string]interface{})
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil, check.Commentf("body: %v", string(body)))
	c.Check(jsonBody["transaction"], check.Equals, string(client.TransactionAllSnaps),
		check.Commentf("body: %v", string(body)))

	_, err = cs.cli.InstallPath(snap, "", &opts)
	c.Assert(err, check.IsNil)

	body, err = io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches,
		"(?s).*Content-Disposition: form-data; name=\"transaction\"\r\n\r\nall-snaps\r\n.*")
}

func (cs *clientSuite) TestClientOpInstallPrefer(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := os.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	opts := client.SnapOptions{
		Prefer: true,
	}

	_, err = cs.cli.Install("foo", &opts)
	c.Assert(err, check.IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var jsonBody map[string]interface{}
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil, check.Commentf("body: %v", string(body)))
	c.Check(jsonBody["prefer"], check.Equals, true, check.Commentf("body: %v", string(body)))

	_, err = cs.cli.InstallPath(snap, "", &opts)
	c.Assert(err, check.IsNil)

	body, err = io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Matches, "(?s).*Content-Disposition: form-data; name=\"prefer\"\r\n\r\ntrue\r\n.*")
}

func formToMap(c *check.C, mr *multipart.Reader) map[string]string {
	formData := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		c.Assert(err, check.IsNil)
		slurp, err := io.ReadAll(p)
		c.Assert(err, check.IsNil)
		formData[p.FormName()] = string(slurp)
	}
	return formData
}

func (cs *clientSuite) TestClientOpTryMode(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	snapdir := filepath.Join(c.MkDir(), "/some/path")

	for _, opts := range []*client.SnapOptions{
		{Classic: false, DevMode: false, JailMode: false},
		{Classic: false, DevMode: false, JailMode: true},
		{Classic: false, DevMode: true, JailMode: true},
		{Classic: false, DevMode: true, JailMode: false},
		{Classic: true, DevMode: false, JailMode: false},
		{Classic: true, DevMode: false, JailMode: true},
		{Classic: true, DevMode: true, JailMode: true},
		{Classic: true, DevMode: true, JailMode: false},
	} {
		comment := check.Commentf("when Classic:%t DevMode:%t JailMode:%t", opts.Classic, opts.DevMode, opts.JailMode)
		id, err := cs.cli.Try(snapdir, opts)
		c.Assert(err, check.IsNil)

		// ensure we send the right form-data
		_, params, err := mime.ParseMediaType(cs.req.Header.Get("Content-Type"))
		c.Assert(err, check.IsNil, comment)
		mr := multipart.NewReader(cs.req.Body, params["boundary"])
		formData := formToMap(c, mr)
		c.Check(formData["action"], check.Equals, "try", comment)
		c.Check(formData["snap-path"], check.Equals, snapdir, comment)
		expectedLength := 2
		if opts.Classic {
			c.Check(formData["classic"], check.Equals, "true", comment)
			expectedLength++
		}
		if opts.DevMode {
			c.Check(formData["devmode"], check.Equals, "true", comment)
			expectedLength++
		}
		if opts.JailMode {
			c.Check(formData["jailmode"], check.Equals, "true", comment)
			expectedLength++
		}
		c.Check(len(formData), check.Equals, expectedLength)

		c.Check(cs.req.Method, check.Equals, "POST", comment)
		c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps", comment)
		c.Assert(cs.req.Header.Get("Content-Type"), check.Matches, "multipart/form-data; boundary=.*", comment)
		c.Check(id, check.Equals, "66b3", comment)
	}
}

func (cs *clientSuite) TestClientOpTryModeDangerous(c *check.C) {
	snapdir := filepath.Join(c.MkDir(), "/some/path")

	_, err := cs.cli.Try(snapdir, &client.SnapOptions{Dangerous: true})
	c.Assert(err, check.Equals, client.ErrDangerousNotApplicable)
}

func (cs *clientSuite) TestSnapOptionsSerialises(c *check.C) {
	tests := map[string]client.SnapOptions{
		"{}":                         {},
		`{"channel":"edge"}`:         {Channel: "edge"},
		`{"revision":"42"}`:          {Revision: "42"},
		`{"cohort-key":"what"}`:      {CohortKey: "what"},
		`{"leave-cohort":true}`:      {LeaveCohort: true},
		`{"devmode":true}`:           {DevMode: true},
		`{"jailmode":true}`:          {JailMode: true},
		`{"classic":true}`:           {Classic: true},
		`{"dangerous":true}`:         {Dangerous: true},
		`{"ignore-validation":true}`: {IgnoreValidation: true},
		`{"unaliased":true}`:         {Unaliased: true},
		`{"purge":true}`:             {Purge: true},
		`{"amend":true}`:             {Amend: true},
		`{"prefer":true}`:            {Prefer: true},
	}
	for expected, opts := range tests {
		buf, err := json.Marshal(&opts)
		c.Assert(err, check.IsNil, check.Commentf("%s", expected))
		c.Check(string(buf), check.Equals, expected)
	}
}

func (cs *clientSuite) TestClientOpDownload(c *check.C) {
	cs.status = 200
	cs.header = http.Header{
		"Content-Disposition": {"attachment; filename=foo_2.snap"},
		"Snap-Sha3-384":       {"sha3sha3sha3"},
		"Snap-Download-Token": {"some-token"},
	}
	cs.contentLength = 1234

	cs.rsp = `lots-of-foo-data`

	dlInfo, rc, err := cs.cli.Download("foo", &client.DownloadOptions{
		SnapOptions: client.SnapOptions{
			Revision: "2",
			Channel:  "edge",
		},
		HeaderPeek: true,
	})
	c.Check(err, check.IsNil)
	c.Check(dlInfo, check.DeepEquals, &client.DownloadInfo{
		SuggestedFileName: "foo_2.snap",
		Size:              1234,
		Sha3_384:          "sha3sha3sha3",
		ResumeToken:       "some-token",
	})

	// check we posted the right stuff
	c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")
	c.Assert(cs.req.Header.Get("range"), check.Equals, "")
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var jsonBody client.DownloadAction
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Check(jsonBody.SnapName, check.DeepEquals, "foo")
	c.Check(jsonBody.Revision, check.Equals, "2")
	c.Check(jsonBody.Channel, check.Equals, "edge")
	c.Check(jsonBody.HeaderPeek, check.Equals, true)

	// ensure we can read the response
	content, err := io.ReadAll(rc)
	c.Assert(err, check.IsNil)
	c.Check(string(content), check.Equals, cs.rsp)
	// and we can close it
	c.Check(rc.Close(), check.IsNil)
}

func (cs *clientSuite) TestClientOpDownloadResume(c *check.C) {
	cs.status = 200
	cs.header = http.Header{
		"Content-Disposition": {"attachment; filename=foo_2.snap"},
		"Snap-Sha3-384":       {"sha3sha3sha3"},
	}
	// we resume
	cs.contentLength = 1234 - 64

	cs.rsp = `lots-of-foo-data`

	dlInfo, rc, err := cs.cli.Download("foo", &client.DownloadOptions{
		SnapOptions: client.SnapOptions{
			Revision: "2",
			Channel:  "edge",
		},
		HeaderPeek:  true,
		ResumeToken: "some-token",
		Resume:      64,
	})
	c.Check(err, check.IsNil)
	c.Check(dlInfo, check.DeepEquals, &client.DownloadInfo{
		SuggestedFileName: "foo_2.snap",
		Size:              1234 - 64,
		Sha3_384:          "sha3sha3sha3",
	})

	// check we posted the right stuff
	c.Assert(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")
	c.Assert(cs.req.Header.Get("range"), check.Equals, "bytes: 64-")
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var jsonBody client.DownloadAction
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Check(jsonBody.SnapName, check.DeepEquals, "foo")
	c.Check(jsonBody.Revision, check.Equals, "2")
	c.Check(jsonBody.Channel, check.Equals, "edge")
	c.Check(jsonBody.HeaderPeek, check.Equals, true)
	c.Check(jsonBody.ResumeToken, check.Equals, "some-token")

	// ensure we can read the response
	content, err := io.ReadAll(rc)
	c.Assert(err, check.IsNil)
	c.Check(string(content), check.Equals, cs.rsp)
	// and we can close it
	c.Check(rc.Close(), check.IsNil)
}

func (cs *clientSuite) TestClientRefreshWithValidationSets(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "12",
		"status-code": 202,
		"type": "async"
	}`

	sets := []string{"foo/bar=2", "foo/baz"}
	chgID, err := cs.cli.RefreshMany(nil, &client.SnapOptions{
		ValidationSets: sets,
	})
	c.Assert(err, check.IsNil)
	c.Check(chgID, check.Equals, "12")

	type req struct {
		ValidationSets []string `json:"validation-sets"`
		Action         string   `json:"action"`
	}
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	var decodedBody req
	err = json.Unmarshal(body, &decodedBody)
	c.Assert(err, check.IsNil)

	c.Check(decodedBody, check.DeepEquals, req{
		ValidationSets: sets,
		Action:         "refresh",
	})
	c.Check(cs.req.Header["Content-Type"], check.DeepEquals, []string{"application/json"})
}

func (cs *clientSuite) TestClientHoldMany(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"change": "12",
		"status-code": 202,
		"type": "async"
	}`

	chgID, err := cs.cli.HoldRefreshesMany([]string{"foo", "bar"}, &client.SnapOptions{
		Time:      "forever",
		HoldLevel: "general",
	})
	c.Assert(err, check.IsNil)
	c.Check(chgID, check.Equals, "12")

	type req struct {
		Action    string   `json:"action"`
		Snaps     []string `json:"snaps"`
		Time      string   `json:"time"`
		HoldLevel string   `json:"hold-level"`
	}
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	var decodedBody req
	err = json.Unmarshal(body, &decodedBody)
	c.Assert(err, check.IsNil)

	c.Check(decodedBody, check.DeepEquals, req{
		Action:    "hold",
		Snaps:     []string{"foo", "bar"},
		Time:      "forever",
		HoldLevel: "general",
	})
	c.Check(cs.req.Header["Content-Type"], check.DeepEquals, []string{"application/json"})
}
