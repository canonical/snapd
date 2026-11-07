// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"bytes"
	"net/http"
	"strings"
	"sync"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
)

var _ = check.Suite(&stacktracesDebugSuite{})

type stacktracesDebugSuite struct {
	apiBaseSuite
}

func (s *stacktracesDebugSuite) getStacktraces(c *check.C) string {
	s.daemonWithOverlordMock()
	s.expectRootAccess()

	buf := bytes.NewBufferString(`{"action": "stacktraces"}`)
	req, err := http.NewRequest("POST", "/v2/debug", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	stacks, ok := rsp.Result.(string)
	c.Assert(ok, check.Equals, true)
	return stacks
}

func parkedGoroutineForStacktracesTest(wg *sync.WaitGroup, stop chan struct{}) {
	wg.Done()
	<-stop
}

func (s *stacktracesDebugSuite) TestStacktracesNotTruncated(c *check.C) {
	// park enough goroutines for their stack traces to exceed the
	// initial buffer size
	const count = 2000
	stop := make(chan struct{})
	defer close(stop)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go parkedGoroutineForStacktracesTest(&wg, stop)
	}
	wg.Wait()

	stacks := s.getStacktraces(c)
	c.Check(len(stacks) > 64*1024, check.Equals, true)
	c.Check(strings.Count(stacks, "daemon_test.parkedGoroutineForStacktracesTest(") >= count, check.Equals, true)
	c.Check(strings.HasSuffix(stacks, "stack traces truncated\n"), check.Equals, false)
}

func (s *stacktracesDebugSuite) TestStacktracesGrowsBuffer(c *check.C) {
	var sizes []int
	restore := daemon.MockStacktraces(func(buf []byte, all bool) int {
		c.Check(all, check.Equals, true)
		sizes = append(sizes, len(buf))
		if len(buf) < 40 {
			// fill up the buffer completely, as runtime.Stack does
			// when the traces do not fit
			return copy(buf, strings.Repeat("x", len(buf)))
		}
		return copy(buf, "all the stacks")
	}, 10, 100)
	defer restore()

	c.Check(s.getStacktraces(c), check.Equals, "all the stacks")
	c.Check(sizes, check.DeepEquals, []int{10, 20, 40})
}

func (s *stacktracesDebugSuite) TestStacktracesUpperBound(c *check.C) {
	var sizes []int
	restore := daemon.MockStacktraces(func(buf []byte, all bool) int {
		sizes = append(sizes, len(buf))
		return copy(buf, strings.Repeat("x", len(buf)))
	}, 10, 50)
	defer restore()

	c.Check(s.getStacktraces(c), check.Equals, strings.Repeat("x", 50)+"\n... stack traces truncated\n")
	c.Check(sizes, check.DeepEquals, []int{10, 20, 40, 50})
}
