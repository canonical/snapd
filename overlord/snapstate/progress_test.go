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

package snapstate

import (
	"github.com/ubuntu-core/snappy/overlord/state"

	. "gopkg.in/check.v1"
)

type progressAdapterTestSuite struct{}

var _ = Suite(&progressAdapterTestSuite{})

func (s *progressAdapterTestSuite) TestProgressAdapterWrite(c *C) {
	st := state.New(nil)
	st.Lock()
	p := TaskProgressAdapter{
		task: st.NewTask("op", "msg"),
	}
	st.Unlock()

	p.Start("msg", 161803)
	c.Check(p.total, Equals, float64(161803))

	p.Write([]byte("some-bytes"))
	c.Check(p.current, Equals, float64(len("some-bytes")))
}
