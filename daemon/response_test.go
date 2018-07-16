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

package daemon

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"
)

type responseSuite struct{}

var _ = check.Suite(&responseSuite{})

func (s *responseSuite) TestRespSetsLocationIfAccepted(c *check.C) {
	rec := httptest.NewRecorder()

	rsp := &resp{
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

	rsp := &resp{
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

	rsp := &resp{
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
	rsp := FileResponse(path)
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
	rj := &respJSON{Result: nil}
	data, err := json.Marshal(rj)
	c.Assert(err, check.IsNil)
	c.Check(string(data), check.Equals, `{"type":"","status-code":0,"status":"","result":null}`)
}

type xT struct {
	called int
	count  int
	stamp  time.Time
}

func (x *xT) WarningsSummary() (int, time.Time) {
	x.called++
	return x.count, x.stamp
}

func (s *responseSuite) TestNewMetaNoWarnings(c *check.C) {
	x := xT{}
	m := newMeta(&x)
	c.Check(x.called, check.Equals, 1)
	// time was zero, so timestamp should be nil
	c.Check(m.WarningCount, check.Equals, 0)
	c.Check(m.WarningTimestamp, check.IsNil)
}

func (s *responseSuite) TestNewMetaSomeWarnings(c *check.C) {
	t := time.Now()
	x := xT{count: 42, stamp: t}
	m := newMeta(&x)
	c.Check(x.called, check.Equals, 1)
	// time was zero, so timestamp should be nil
	c.Check(m.WarningCount, check.Equals, 42)
	c.Check(m.WarningTimestamp, check.DeepEquals, &t)
}
