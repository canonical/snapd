// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package daemon_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type responseSuite struct{}

var _ = check.Suite(&responseSuite{})

func (s *responseSuite) TestRespSetsLocationIfAccepted(c *check.C) {
	rec := httptest.NewRecorder()

	rsp := &daemon.Resp{
		Status: 202,
		Result: map[string]interface{}{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Header()
	c.Check(hdr.Get("Location"), check.Equals, "foo/bar")
}

func (s *responseSuite) TestRespSetsLocationIfCreated(c *check.C) {
	rec := httptest.NewRecorder()

	rsp := &daemon.Resp{
		Status: 201,
		Result: map[string]interface{}{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Header()
	c.Check(hdr.Get("Location"), check.Equals, "foo/bar")
}

func (s *responseSuite) TestRespDoesNotSetLocationIfOther(c *check.C) {
	rec := httptest.NewRecorder()

	rsp := &daemon.Resp{
		Status: 418, // I'm a teapot
		Result: map[string]interface{}{
			"resource": "foo/bar",
		},
	}

	rsp.ServeHTTP(rec, nil)
	hdr := rec.Header()
	c.Check(hdr.Get("Location"), check.Equals, "")
}

func (s *responseSuite) TestFileResponseSetsContentDisposition(c *check.C) {
	const filename = "icon.png"

	path := filepath.Join(c.MkDir(), filename)
	err := ioutil.WriteFile(path, nil, os.ModePerm)
	c.Check(err, check.IsNil)

	rec := httptest.NewRecorder()
	rsp := daemon.FileResponse(path)
	req, err := http.NewRequest("GET", "", nil)
	c.Check(err, check.IsNil)

	rsp.ServeHTTP(rec, req)

	hdr := rec.Header()
	c.Check(hdr.Get("Content-Disposition"), check.Equals,
		fmt.Sprintf("attachment; filename=%s", filename))
}

// Due to how the protocol was defined the result must be sent, even if it is
// null. Older clients rely on this.
func (s *responseSuite) TestRespJSONWithNullResult(c *check.C) {
	rj := &daemon.RespJSON{Result: nil}
	data, err := json.Marshal(rj)
	c.Assert(err, check.IsNil)
	c.Check(string(data), check.Equals, `{"type":"","status-code":0,"status":"","result":null}`)
}

func (responseSuite) TestErrorResponderPrintfsWithArgs(c *check.C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below %d%%.", 1)
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result daemon.ErrorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), check.IsNil)

	c.Check(v.Result.Message, check.Equals, "system memory below 1%.")
}

func (responseSuite) TestErrorResponderDoesNotPrintfAlways(c *check.C) {
	teapot := daemon.MakeErrorResponder(418)

	rec := httptest.NewRecorder()
	rsp := teapot("system memory below 1%.")
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	rsp.ServeHTTP(rec, req)

	var v struct{ Result daemon.ErrorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), check.IsNil)

	c.Check(v.Result.Message, check.Equals, "system memory below 1%.")
}

type fakeNetError struct {
	message   string
	timeout   bool
	temporary bool
}

func (e fakeNetError) Error() string   { return e.message }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.temporary }

func (s *responseSuite) TestErrToResponse(c *check.C) {
	aie := &snap.AlreadyInstalledError{Snap: "foo"}
	nie := &snap.NotInstalledError{Snap: "foo"}
	cce := &snapstate.ChangeConflictError{Snap: "foo"}
	ndme := &snapstate.SnapNeedsDevModeError{Snap: "foo"}
	nc := &snapstate.SnapNotClassicError{Snap: "foo"}
	nce := &snapstate.SnapNeedsClassicError{Snap: "foo"}
	ncse := &snapstate.SnapNeedsClassicSystemError{Snap: "foo"}
	netoe := fakeNetError{message: "other"}
	nettoute := fakeNetError{message: "timeout", timeout: true}
	nettmpe := fakeNetError{message: "temp", temporary: true}

	e := errors.New("other error")

	sa1e := &store.SnapActionError{Refresh: map[string]error{"foo": store.ErrSnapNotFound}}
	sa2e := &store.SnapActionError{Refresh: map[string]error{
		"foo": store.ErrSnapNotFound,
		"bar": store.ErrSnapNotFound,
	}}
	saOe := &store.SnapActionError{Other: []error{e}}
	// this one can't happen (but fun to test):
	saXe := &store.SnapActionError{Refresh: map[string]error{"foo": sa1e}}

	makeErrorRsp := func(kind client.ErrorKind, err error, value interface{}) *daemon.APIError {
		return &daemon.APIError{
			Status:  400,
			Message: err.Error(),
			Kind:    kind,
			Value:   value,
		}
	}

	tests := []struct {
		err         error
		expectedRsp daemon.Response
	}{
		{store.ErrSnapNotFound, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		{store.ErrNoUpdateAvailable, makeErrorRsp(client.ErrorKindSnapNoUpdateAvailable, store.ErrNoUpdateAvailable, "")},
		{store.ErrLocalSnap, makeErrorRsp(client.ErrorKindSnapLocal, store.ErrLocalSnap, "")},
		{aie, makeErrorRsp(client.ErrorKindSnapAlreadyInstalled, aie, "foo")},
		{nie, makeErrorRsp(client.ErrorKindSnapNotInstalled, nie, "foo")},
		{ndme, makeErrorRsp(client.ErrorKindSnapNeedsDevMode, ndme, "foo")},
		{nc, makeErrorRsp(client.ErrorKindSnapNotClassic, nc, "foo")},
		{nce, makeErrorRsp(client.ErrorKindSnapNeedsClassic, nce, "foo")},
		{ncse, makeErrorRsp(client.ErrorKindSnapNeedsClassicSystem, ncse, "foo")},
		{cce, daemon.SnapChangeConflict(cce)},
		{nettoute, makeErrorRsp(client.ErrorKindNetworkTimeout, nettoute, "")},
		{netoe, daemon.BadRequest("ERR: %v", netoe)},
		{nettmpe, daemon.BadRequest("ERR: %v", nettmpe)},
		{e, daemon.BadRequest("ERR: %v", e)},

		// action error unwrapping:
		{sa1e, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		{saXe, daemon.SnapNotFound("foo", store.ErrSnapNotFound)},
		// action errors, unwrapped:
		{sa2e, daemon.BadRequest(`ERR: cannot refresh: snap not found: "bar", "foo"`)},
		{saOe, daemon.BadRequest("ERR: cannot refresh, install, or download: other error")},
	}

	for _, t := range tests {
		com := check.Commentf("%v", t.err)
		rspe := daemon.ErrToResponse(t.err, []string{"foo"}, daemon.BadRequest, "%s: %v", "ERR")
		c.Check(rspe, check.DeepEquals, t.expectedRsp, com)
	}
}

func (s *responseSuite) TestErrToResponseInsufficentSpace(c *check.C) {
	err := &snapstate.InsufficientSpaceError{
		Snaps:      []string{"foo", "bar"},
		ChangeKind: "some-change",
		Path:       "/path",
		Message:    "specific error msg",
	}
	rspe := daemon.ErrToResponse(err, nil, daemon.BadRequest, "%s: %v", "ERR")
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  507,
		Message: "specific error msg",
		Kind:    client.ErrorKindInsufficientDiskSpace,
		Value: map[string]interface{}{
			"snap-names":  []string{"foo", "bar"},
			"change-kind": "some-change",
		},
	})
}
