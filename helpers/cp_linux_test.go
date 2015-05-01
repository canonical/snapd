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

package helpers

import (
	"io/ioutil"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func (s *cpSuite) TestCpMulti(c *C) {
	maxcp = 2
	defer func() { maxcp = maxint }()

	d := c.MkDir()
	f1 := filepath.Join(d, "f1")
	f2 := filepath.Join(d, "f2")
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	c.Assert(ioutil.WriteFile(f1, data, 0644), IsNil)
	c.Check(CopyFile(f1, f2, CopyFlagDefault), IsNil)
	bs, err := ioutil.ReadFile(f2)
	c.Check(err, IsNil)
	c.Check(bs, DeepEquals, data)
}
