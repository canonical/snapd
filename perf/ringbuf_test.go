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

package perf_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/perf"
)

type ringbufSuite struct{}

var _ = Suite(&ringbufSuite{})

func (*ringbufSuite) TestNewRingBuffer(c *C) {
	buf := perf.NewRingBuffer(10)
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 0)
	c.Check(len(buf.Data()), Equals, 10)
	c.Check(cap(buf.Data()), Equals, 10)
}

func (*ringbufSuite) TestStoreAndSamples(c *C) {
	buf := perf.NewRingBuffer(3)
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 0)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	c.Check(buf.Samples(), DeepEquals, []perf.Sample{})

	// store "a"
	buf.Store(&perf.Sample{Summary: "a"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "a"}, {}, {}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "a"}})
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 1)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "b"
	buf.Store(&perf.Sample{Summary: "b"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "a"}, {Summary: "b"}, {}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "a"}, {Summary: "b"}})
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 2)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "c"
	buf.Store(&perf.Sample{Summary: "c"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "a"}, {Summary: "b"}, {Summary: "c"}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "a"}, {Summary: "b"}, {Summary: "c"}})
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 3)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "d"
	buf.Store(&perf.Sample{Summary: "d"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "d"}, {Summary: "b"}, {Summary: "c"}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "b"}, {Summary: "c"}, {Summary: "d"}})
	c.Check(buf.Start(), Equals, 1)
	c.Check(buf.Count(), Equals, 3)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "e"
	buf.Store(&perf.Sample{Summary: "e"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "d"}, {Summary: "e"}, {Summary: "c"}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "c"}, {Summary: "d"}, {Summary: "e"}})
	c.Check(buf.Start(), Equals, 2)
	c.Check(buf.Count(), Equals, 3)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "f"
	buf.Store(&perf.Sample{Summary: "f"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "d"}, {Summary: "e"}, {Summary: "f"}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "d"}, {Summary: "e"}, {Summary: "f"}})
	c.Check(buf.Start(), Equals, 0)
	c.Check(buf.Count(), Equals, 3)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)

	// store "g"
	buf.Store(&perf.Sample{Summary: "g"})
	c.Check(buf.Data(), DeepEquals, []perf.Sample{{Summary: "g"}, {Summary: "e"}, {Summary: "f"}})
	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{Summary: "e"}, {Summary: "f"}, {Summary: "g"}})
	c.Check(buf.Start(), Equals, 1)
	c.Check(buf.Count(), Equals, 3)
	c.Check(len(buf.Data()), Equals, 3)
	c.Check(cap(buf.Data()), Equals, 3)
}

// StoreMany works like atomic sequence of Store.
func (*ringbufSuite) TestStoreMany(c *C) {
	buf := perf.NewRingBuffer(2)
	buf.Store(&perf.Sample{ID: 1})
	buf.StoreMany([]perf.Sample{{ID: 2}, {ID: 3}, {ID: 4}})

	c.Check(buf.Samples(), DeepEquals, []perf.Sample{{ID: 3}, {ID: 4}})
}

// Filter can return a subset of samples.
func (*ringbufSuite) TestFilter(c *C) {
	buf := perf.NewRingBuffer(100)
	for i := 0; i < 10; i++ {
		buf.Store(&perf.Sample{ID: uint64(i)})
	}
	odd := buf.Filter(func(s *perf.Sample) bool { return s.ID%2 == 1 })
	c.Check(odd, DeepEquals, []perf.Sample{{ID: 1}, {ID: 3}, {ID: 5}, {ID: 7}, {ID: 9}})
}

// Using a nil buffer is not a problem.
func (*ringbufSuite) TestNilBuffer(c *C) {
	var buf *perf.RingBuffer
	c.Check(buf, IsNil)
	buf.Store(&perf.Sample{})
	buf.StoreMany([]perf.Sample{{}})
	c.Check(buf.Samples(), HasLen, 0)
	c.Check(buf.Filter(func(*perf.Sample) bool { return true }), HasLen, 0)
}
