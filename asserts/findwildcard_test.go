// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/check.v1"
)

type findWildcardSuite struct{}

var _ = check.Suite(&findWildcardSuite{})

func (fs *findWildcardSuite) TestFindWildcard(c *check.C) {
	top := filepath.Join(c.MkDir(), "top")

	err := os.MkdirAll(top, os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id1"), os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id1", "abcd"), os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id1", "e5cd"), os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id2"), os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id2", "f444"), os.ModePerm)
	c.Assert(err, check.IsNil)

	err = ioutil.WriteFile(filepath.Join(top, "acc-id1", "abcd", "active"), nil, os.ModePerm)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(filepath.Join(top, "acc-id1", "abcd", "active.1"), nil, os.ModePerm)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(filepath.Join(top, "acc-id1", "e5cd", "active"), nil, os.ModePerm)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(filepath.Join(top, "acc-id2", "f444", "active"), nil, os.ModePerm)
	c.Assert(err, check.IsNil)

	var res []string
	foundCb := func(relpath []string) error {
		res = append(res, relpath...)
		return nil
	}

	err = findWildcard(top, []string{"*", "*", "active"}, foundCb)
	c.Assert(err, check.IsNil)
	sort.Strings(res)
	c.Check(res, check.DeepEquals, []string{"acc-id1/abcd/active", "acc-id1/e5cd/active", "acc-id2/f444/active"})

	res = nil
	err = findWildcard(top, []string{"*", "*", "active*"}, foundCb)
	c.Assert(err, check.IsNil)
	sort.Strings(res)
	c.Check(res, check.DeepEquals, []string{"acc-id1/abcd/active", "acc-id1/abcd/active.1", "acc-id1/e5cd/active", "acc-id2/f444/active"})

	res = nil
	err = findWildcard(top, []string{"zoo", "*", "active"}, foundCb)
	c.Assert(err, check.IsNil)
	c.Check(res, check.HasLen, 0)

	res = nil
	err = findWildcard(top, []string{"zoo", "*", "active*"}, foundCb)
	c.Assert(err, check.IsNil)
	c.Check(res, check.HasLen, 0)

	res = nil
	err = findWildcard(top, []string{"a*", "zoo", "active"}, foundCb)
	c.Assert(err, check.IsNil)
	c.Check(res, check.HasLen, 0)

	res = nil
	err = findWildcard(top, []string{"acc-id1", "*cd", "active"}, foundCb)
	c.Assert(err, check.IsNil)
	sort.Strings(res)
	c.Check(res, check.DeepEquals, []string{"acc-id1/abcd/active", "acc-id1/e5cd/active"})

	res = nil
	err = findWildcard(top, []string{"acc-id1", "*cd", "active*"}, foundCb)
	c.Assert(err, check.IsNil)
	sort.Strings(res)
	c.Check(res, check.DeepEquals, []string{"acc-id1/abcd/active", "acc-id1/abcd/active.1", "acc-id1/e5cd/active"})

}

func (fs *findWildcardSuite) TestFindWildcardSomeErrors(c *check.C) {
	top := filepath.Join(c.MkDir(), "top-errors")

	err := os.MkdirAll(top, os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id1"), os.ModePerm)
	c.Assert(err, check.IsNil)
	err = os.MkdirAll(filepath.Join(top, "acc-id2"), os.ModePerm)
	c.Assert(err, check.IsNil)

	err = ioutil.WriteFile(filepath.Join(top, "acc-id1", "abcd"), nil, os.ModePerm)
	c.Assert(err, check.IsNil)

	err = os.MkdirAll(filepath.Join(top, "acc-id2", "dddd"), os.ModePerm)
	c.Assert(err, check.IsNil)

	var res []string
	var retErr error
	foundCb := func(relpath []string) error {
		res = append(res, relpath...)
		return retErr
	}

	myErr := errors.New("boom")
	retErr = myErr
	err = findWildcard(top, []string{"acc-id1", "*"}, foundCb)
	c.Check(err, check.Equals, myErr)

	retErr = nil
	res = nil
	err = findWildcard(top, []string{"acc-id2", "*"}, foundCb)
	c.Check(err, check.ErrorMatches, "expected a regular file: .*")
}
