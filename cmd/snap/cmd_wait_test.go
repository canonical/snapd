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
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCmdWaitHappy(c *C) {
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

		fmt.Fprintln(w, fmt.Sprintf(`{"type":"sync", "status-code": 200, "result": {"seed.loaded":%v}}`, seeded))
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"wait", "system", "seed.loaded"})
	c.Assert(err, IsNil)

	// ensure we retried a bit but make the check not overly precise
	// because this will run in super busy build hosts that where a
	// 10 millisecond sleep actually takes much longer until the kernel
	// hands control back to the process
	c.Check(n > 2, Equals, true)
}

func (s *SnapSuite) TestCmdWaitMissingConfKey(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"wait", "snapName"})
	c.Assert(err, ErrorMatches, "need a <key> argument")

	c.Check(n, Equals, 0)
}

func (s *SnapSuite) TestTrueish(c *C) {
	tests := []struct {
		v interface{}
		b bool
	}{
		// bool
		{true, true},
		{false, false},
		// int
		{int(1), true},
		{int(2), true},
		{int(0), false},
		// int8
		{int8(1), true},
		{int8(2), true},
		{int8(0), false},
		// int16
		{int16(1), true},
		{int16(2), true},
		{int16(0), false},
		// int32
		{int32(1), true},
		{int32(2), true},
		{int32(0), false},
		// int64
		{int64(1), true},
		{int64(2), true},
		{int64(0), false},
		// uint
		{uint(1), true},
		{uint(2), true},
		{uint(0), false},
		// uint8
		{uint8(1), true},
		{uint8(2), true},
		{uint8(0), false},
		// uint16
		{uint16(1), true},
		{uint16(2), true},
		{uint16(0), false},
		// uint32
		{uint32(1), true},
		{uint32(2), true},
		{uint32(0), false},
		// uint64
		{uint64(1), true},
		{uint64(2), true},
		{uint64(0), false},
		// uintptr
		{uintptr(1), true},
		{uintptr(2), true},
		{uintptr(0), false},
		// byte
		{byte(1), true},
		{byte(2), true},
		{byte(0), false},
		// rune
		{rune(1), true},
		{rune(2), true},
		{rune(0), false},
		// float32
		{float32(1.0), true},
		{float32(2.0), true},
		{float32(0.0), false},
		// float64
		{float64(1.0), true},
		{float64(2.0), true},
		{float64(0.0), false},
		// no complex{64,128}
		// ...
		// string
		{"a", true},
		{"", false},
		// json.Number
		{json.Number("1"), true},
		{json.Number("0"), false},
		{json.Number("1.0"), true},
		{json.Number("0.0"), false},
		// slices
		{[]interface{}{"a"}, true},
		{[]interface{}{}, false},
		{[]string{"a"}, true},
		{[]string{}, false},
		// arrays
		{[2]interface{}{"a", "b"}, true},
		{[0]interface{}{}, false},
		{[2]string{"a", "b"}, true},
		{[0]string{}, false},
		// maps
		{map[string]interface{}{"a": "a"}, true},
		{map[string]interface{}{}, false},
		{map[interface{}]interface{}{"a": "a"}, true},
		{map[interface{}]interface{}{}, false},
	}
	for _, t := range tests {
		c.Check(snap.Trueish(t.v), Equals, t.b, Commentf("unexpected result for %v (%T), did not get expected %v", t.v, t.v, t.b))
	}
}
