// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0
package config_test

import (
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/canonical/telem-agent/config"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type configSuite struct {
	options env.Options
}

var _ = Suite(&configSuite{})

func (cfg *configSuite) SetUpSuite(c *C) {
	cfg.options = env.Options{}
}

func (cfg *configSuite) TestCreatingNewConfig(c *C) {
	config, err := config.NewConfig(cfg.options)

	c.Check(err, IsNil)
	c.Check(config, NotNil)
}
