// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"io/ioutil"
	"net/http"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/logger"
)

var mockMountInfoFmt = `
24 0 8:18 / %s rw,relatime shared:1 - ext4 /dev/sdb2 rw,errors=remount-ro,data=ordered
`

func makeMockMountInfo(c *C, content string) string {
	fn := filepath.Join(c.MkDir(), "mountinfo")
	err := ioutil.WriteFile(fn, []byte(content), 0644)
	c.Assert(err, IsNil)
	return fn
}

func (s *SnapSuite) TestAutoImportAssertsHappy(c *C) {
	fakeAssertData := []byte("my-assertion")

	n := 0
	total := 1
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(postData, DeepEquals, fakeAssertData)
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
			n++
		default:
			c.Fatalf("unexpected request: %v (expected %d got %d)", r, total, n)
		}

	})

	fakeAssertsFn := filepath.Join(c.MkDir(), "auto-imports.assert")
	err := ioutil.WriteFile(fakeAssertsFn, fakeAssertData, 0644)
	c.Assert(err, IsNil)

	content := fmt.Sprintf(mockMountInfoFmt, filepath.Dir(fakeAssertsFn))
	snap.MockMountInfoPath(makeMockMountInfo(c, content))

	l, err := logger.NewConsoleLog(s.stderr, 0)
	c.Assert(err, IsNil)
	logger.SetLogger(l)

	rest, err := snap.Parser().ParseArgs([]string{"auto-import"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, fmt.Sprintf("acked %q\n", fakeAssertsFn))
	c.Check(n, Equals, total)
}
