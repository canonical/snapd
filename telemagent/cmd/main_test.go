// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package main_test

import (
	"context"
	"testing"
	"time"

	main "github.com/snapcore/snapd/telemagent/cmd"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct{}

var _ = Suite(&cmdSuite{})

func (cs *cmdSuite) SetUpSuite(c *C) {
}

func (cs *cmdSuite) TestMainFunc(c *C) {
	_, cancel := context.WithCancel(context.Background())

	go main.Main()

	time.Sleep(2 * time.Second)
	cancel()
}
