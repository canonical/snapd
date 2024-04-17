// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clientutil_test

import (
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	. "gopkg.in/check.v1"
)

type serviceScopeSuite struct{}

var _ = Suite(&serviceScopeSuite{})

func (s *serviceScopeSuite) TestScopes(c *C) {
	tests := []struct {
		opts     clientutil.ServiceScopeOptions
		expected client.ScopeSelector
	}{
		// when expected is nil it means both scopes
		{clientutil.ServiceScopeOptions{}, nil},
		{clientutil.ServiceScopeOptions{User: true}, client.ScopeSelector{"user"}},
		{clientutil.ServiceScopeOptions{Usernames: "all"}, client.ScopeSelector{"user"}},
		{clientutil.ServiceScopeOptions{System: true}, client.ScopeSelector{"system"}},
		{clientutil.ServiceScopeOptions{User: true, System: true}, nil},
		{clientutil.ServiceScopeOptions{Usernames: "all", System: true}, nil},
	}

	for _, t := range tests {
		c.Check(t.opts.Scope(), DeepEquals, t.expected)
	}
}

func (s *serviceScopeSuite) TestUsers(c *C) {
	tests := []struct {
		opts     clientutil.ServiceScopeOptions
		expected client.UserSelector
	}{
		{clientutil.ServiceScopeOptions{}, client.UserSelector{Names: []string{}, Selector: client.UserSelectionList}},
		{clientutil.ServiceScopeOptions{User: true}, client.UserSelector{Selector: client.UserSelectionSelf}},
		{clientutil.ServiceScopeOptions{Usernames: "all"}, client.UserSelector{Selector: client.UserSelectionAll}},
		{clientutil.ServiceScopeOptions{System: true}, client.UserSelector{Names: []string{}, Selector: client.UserSelectionList}},
		{clientutil.ServiceScopeOptions{User: true, System: true}, client.UserSelector{Selector: client.UserSelectionSelf}},
		{clientutil.ServiceScopeOptions{Usernames: "all", System: true}, client.UserSelector{Selector: client.UserSelectionAll}},
	}

	for _, t := range tests {
		c.Check(t.opts.Users(), DeepEquals, t.expected)
	}
}

func (s *serviceScopeSuite) TestInvalidOptions(c *C) {
	tests := []struct {
		opts     clientutil.ServiceScopeOptions
		expected string
	}{
		{clientutil.ServiceScopeOptions{Usernames: "foo"}, `only "all" is supported as a value for --users`},
		{clientutil.ServiceScopeOptions{User: true, System: true}, `--system and --user cannot be used in conjunction with each other`},
		{clientutil.ServiceScopeOptions{Usernames: "all", User: true}, `--user and --users cannot be used in conjunction with each other`},
	}

	for _, t := range tests {
		c.Check(t.opts.Validate(), ErrorMatches, t.expected)
	}
}
