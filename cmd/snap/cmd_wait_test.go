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

package main_test

import (
	"fmt"
	"net/http"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCmdWait(c *C) {
	var seeded bool

	restore := snap.MockWaitConfTimeout(10 * time.Millisecond)
	defer restore()

	go func() {
		time.Sleep(50 * time.Millisecond)
		seeded = true
	}()
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/snaps/system/conf")

		fmt.Fprintln(w, fmt.Sprintf(`{"type":"sync", "status-code": 200, "result": {"seed.done":%v}}`, seeded))
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"wait", "system", "seed.done"})
	c.Assert(err, IsNil)

	// ensure we retried a bit but make the check not overly precise
	// because this will run in super busy build hosts that where a
	// 10 millisecond sleep actually takes much longer until the kernel
	// hands control back to the process
	c.Check(n > 2, Equals, true)
}
