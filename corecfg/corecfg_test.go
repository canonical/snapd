// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg_test

import (
	"fmt"
	"reflect"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
)

func Test(t *testing.T) { TestingT(t) }

type mockConf struct {
	conf map[string]interface{}
	err  error
}

func (cfg *mockConf) Get(snapName, key string, result interface{}) error {
	if snapName != "core" {
		return fmt.Errorf("mockConf only knows about core")
	}
	if cfg.conf[key] != nil {
		v1 := reflect.ValueOf(result)
		v2 := reflect.Indirect(v1)
		v2.Set(reflect.ValueOf(cfg.conf[key]))
	}
	return cfg.err
}

// coreCfgSuite is the base for all the corecfg tests
type coreCfgSuite struct {
	systemctlArgs     [][]string
	systemctlRestorer func()
}

var _ = Suite(&coreCfgSuite{})

func (s *coreCfgSuite) SetUpSuite(c *C) {
	s.systemctlRestorer = systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, args[:])
		output := []byte("ActiveState=inactive")
		return output, nil
	})
}

func (s *coreCfgSuite) TearDownSuite(c *C) {
	s.systemctlRestorer()
}

// runCfgSuite tests corecfg.Run()
type runCfgSuite struct {
	coreCfgSuite
}
