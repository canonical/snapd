// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"errors"
	"io/ioutil"
	"os"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type FakeClient struct {
	client.Client

	mockedList func(names []string, opts *client.ListOptions) ([]*client.Snap, error)
	mockedFind func(opts *client.FindOptions) ([]*client.Snap, *client.ResultInfo, error)
}

func (c *FakeClient) List(names []string, opts *client.ListOptions) ([]*client.Snap, error) {
	return c.mockedList(names, opts)
}

func (c *FakeClient) Find(opts *client.FindOptions) ([]*client.Snap, *client.ResultInfo, error) {
	return c.mockedFind(opts)
}

func mockSnapNamesFile(filePath string) (restore func()) {
	old := dirs.SnapNamesFile
	dirs.SnapNamesFile = filePath
	return func() {
		dirs.SnapNamesFile = old
	}
}

type completeSuite struct {
	testutil.BaseTest

	fakeClient *FakeClient
}

var _ = Suite(&completeSuite{})

func (cs *completeSuite) SetUpTest(c *C) {
	cs.fakeClient = &FakeClient{}
	cs.AddCleanup(client.MockNewClient(func(conf *client.Config) client.Client {
		return cs.fakeClient
	}))
}

func (cs *completeSuite) TestInstalledSnapNameHappy(c *C) {
	allSnaps := []*client.Snap{
		{Name: "bar"},
		{Name: "fooish"},
		{Name: "fodoesntmatch"},
		{Name: "foo"},
	}
	cs.fakeClient.mockedList = func(names []string, opts *client.ListOptions) ([]*client.Snap, error) {
		c.Check(names, IsNil)
		c.Check(opts, IsNil)
		return allSnaps, nil
	}

	expectedCompletions := []flags.Completion{
		{Item: "fooish"},
		{Item: "foo"},
	}
	completions := main.InstalledSnapNameCompletion("foo")
	c.Check(completions, DeepEquals, expectedCompletions)
}

func (cs *completeSuite) TestInstalledSnapNameError(c *C) {
	cs.fakeClient.mockedList = func(names []string, opts *client.ListOptions) ([]*client.Snap, error) {
		c.Check(names, IsNil)
		c.Check(opts, IsNil)
		return nil, errors.New("listing error")
	}

	completions := main.InstalledSnapNameCompletion("foo")
	c.Check(completions, HasLen, 0)
}

func (cs *completeSuite) TestRemoteSnapNameHappy(c *C) {
	allSnaps := []*client.Snap{
		{Name: "bar"},
		{Name: "no-local-filtering"},
	}
	matchString := "bar"
	cs.fakeClient.mockedFind = func(opts *client.FindOptions) ([]*client.Snap, *client.ResultInfo, error) {
		c.Check(opts.Query, Equals, matchString)
		c.Check(opts.Prefix, Equals, true)
		return allSnaps, nil, nil
	}

	mockSnapNamesFile("/i/do/not/exist")
	expectedCompletions := []flags.Completion{
		{Item: "bar"},
		{Item: "no-local-filtering"},
	}
	completions := main.RemoteSnapNameCompletion("bar")
	c.Check(completions, DeepEquals, expectedCompletions)
}

func (cs *completeSuite) TestRemoteSnapNameFromLocalFileHappy(c *C) {
	file, err := ioutil.TempFile("", "complete_test_*.txt")
	c.Assert(err, IsNil)
	defer os.Remove(file.Name())

	_, err = file.WriteString("aaa\nbar\nbarbecue\nbbq\n")
	c.Assert(err, IsNil)

	mockSnapNamesFile(file.Name())
	expectedCompletions := []flags.Completion{
		{Item: "bar"},
		{Item: "barbecue"},
	}
	completions := main.RemoteSnapNameCompletion("bar")
	c.Check(completions, DeepEquals, expectedCompletions)
}

func (cs *completeSuite) TestRemoteSnapNameTooShort(c *C) {
	mockSnapNamesFile("/i/do/not/exist")
	completions := main.RemoteSnapNameCompletion("me")
	c.Check(completions, HasLen, 0)
}

func (cs *completeSuite) TestRemoteSnapNameError(c *C) {
	cs.fakeClient.mockedFind = func(opts *client.FindOptions) ([]*client.Snap, *client.ResultInfo, error) {
		return nil, nil, errors.New("find error")
	}

	mockSnapNamesFile("/i/do/not/exist")
	completions := main.RemoteSnapNameCompletion("bar")
	c.Check(completions, HasLen, 0)
}
