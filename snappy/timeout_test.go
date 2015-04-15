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

package snappy

import (
	"encoding/json"
	"time"

	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestTimeoutMarshal(c *C) {
	bs, err := Timeout(DefaultTimeout).MarshalJSON()
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, `"30s"`)
}

type testT struct {
	T Timeout
}

func (s *SnapTestSuite) TestTimeoutMarshalIndirect(c *C) {
	bs, err := json.Marshal(testT{DefaultTimeout})
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, `{"T":"30s"}`)
}

func (s *SnapTestSuite) TestTimeoutUnmarshal(c *C) {
	var t testT
	c.Assert(json.Unmarshal([]byte(`{"T": "17ms"}`), &t), IsNil)
	c.Check(t, DeepEquals, testT{T: Timeout(17 * time.Millisecond)})
}
