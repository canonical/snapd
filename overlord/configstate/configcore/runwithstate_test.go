// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2023 Canonical Ltd
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

package configcore_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func (s *configcoreSuite) TestNilHandleWithStateHandlerPanic(c *C) {
	c.Assert(func() { configcore.AddWithStateHandler(nil, nil, nil) },
		Panics, "cannot have nil handle with addWithStateHandler if validatedOnlyStateConfig flag is not set")
}

func (r *configcoreSuite) TestConfigureUnknownOption(c *C) {
	conf := &mockConf{
		state: r.state,
		changes: map[string]any{
			"unknown.option": "1",
		},
	}

	err := configcore.Run(coreDev, conf)
	c.Check(err, ErrorMatches, `cannot set "core.unknown.option": unsupported system option`)
}

type graduatedSuite struct {
	configcoreSuite
}

var _ = Suite(&graduatedSuite{})

func (r *graduatedSuite) SetUpTest(c *C) {
	r.configcoreSuite.SetUpTest(c)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "etc", "environment"), nil, 0644), IsNil)
}

func (r *graduatedSuite) TestConfigureGraduatedExperimentalFeature(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	r.state.Lock()
	defer r.state.Unlock()

	task := r.state.NewTask("configure", "configure")
	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), task)

	for _, feature := range features.Graduated() {
		c.Assert(tr.Set("core", "experimental."+feature, true), IsNil)
	}
	c.Assert(tr.Set("core", "experimental.parallel-instances", true), IsNil)

	r.state.Unlock()
	c.Assert(configcore.Run(coreDev, tr), IsNil)
	r.state.Lock()

	tr.Commit()

	for _, feature := range features.Graduated() {
		msg := "feature " + feature + " is no longer experimental and is always enabled"

		var value any
		err := tr.Get("core", "experimental."+feature, &value)
		c.Check(config.IsNoOption(err), Equals, true)

		c.Check(logbuf.String(), testutil.Contains, msg)
		c.Check(strings.Join(task.Log(), "\n"), testutil.Contains, msg)
		c.Check(warningsStrings(r.state.AllWarnings()), testutil.Contains, msg)
	}

	var parallelInstances bool
	err := tr.Get("core", "experimental.parallel-instances", &parallelInstances)
	c.Check(err, IsNil)
	c.Check(parallelInstances, Equals, true)

}

func (r *graduatedSuite) TestConfigureDefaultEnabledExperimentalFeature(c *C) {
	r.state.Lock()
	defer r.state.Unlock()

	task := r.state.NewTask("configure", "configure")
	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), task)

	c.Assert(tr.Set("core", "experimental.quota-groups", true), IsNil)

	r.state.Unlock()
	c.Assert(configcore.Run(coreDev, tr), IsNil)
	r.state.Lock()

	tr.Commit()

	msg := "feature quota-groups is enabled by default and will be permanently enabled in a future release"

	var enabled bool
	err := tr.Get("core", "experimental.quota-groups", &enabled)
	c.Check(err, IsNil)
	c.Check(enabled, Equals, true)

	c.Check(strings.Join(task.Log(), "\n"), testutil.Contains, msg)
	c.Check(warningsStrings(r.state.AllWarnings()), testutil.Contains, msg)
}

func (r *graduatedSuite) TestConfigureGraduatedExperimentalFeatureDeletesExistingConfig(c *C) {
	r.state.Lock()
	defer r.state.Unlock()

	setupTr := config.NewTransaction(r.state)
	for _, feature := range features.Graduated() {
		c.Assert(setupTr.Set("core", "experimental."+feature, true), IsNil)
	}
	setupTr.Commit()

	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), nil)
	for _, feature := range features.Graduated() {
		c.Assert(tr.Set("core", "experimental."+feature, false), IsNil)
	}

	r.state.Unlock()
	c.Assert(configcore.Run(coreDev, tr), IsNil)
	r.state.Lock()

	tr.Commit()

	for _, feature := range features.Graduated() {
		var value any
		err := tr.Get("core", "experimental."+feature, &value)
		c.Check(config.IsNoOption(err), Equals, true)
	}
}

func (r *graduatedSuite) TestPruneGraduatedExperimentalConfig(c *C) {
	r.state.Lock()
	defer r.state.Unlock()

	setupTr := config.NewTransaction(r.state)
	for _, feature := range features.Graduated() {
		c.Assert(setupTr.Set("core", "experimental."+feature, true), IsNil)
	}
	c.Assert(setupTr.Set("core", "experimental.parallel-instances", true), IsNil)
	setupTr.Commit()

	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), nil)
	c.Assert(configcore.PruneGraduatedExperimentalConfig(tr), IsNil)
	tr.Commit()

	for _, feature := range features.Graduated() {
		var value any
		err := tr.Get("core", "experimental."+feature, &value)
		c.Check(config.IsNoOption(err), Equals, true)
	}

	var enabled bool
	err := tr.Get("core", "experimental.parallel-instances", &enabled)
	c.Check(err, IsNil)
	c.Check(enabled, Equals, true)
}

func (r *graduatedSuite) TestPruneGraduatedExperimentalConfigDoesNotCreateConfig(c *C) {
	r.state.Lock()
	defer r.state.Unlock()

	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), nil)
	c.Assert(configcore.PruneGraduatedExperimentalConfig(tr), IsNil)
	tr.Commit()

	rawCfg, err := config.GetSnapConfig(r.state, "core")
	c.Check(err, IsNil)
	c.Check(rawCfg, IsNil)
}

func (r *graduatedSuite) TestConfigureUnknownExperimentalFeatureError(c *C) {
	r.state.Lock()
	defer r.state.Unlock()

	tr := configcore.NewRunTransaction(config.NewTransaction(r.state), nil)
	c.Assert(tr.Set("core", "experimental.not-graduated", true), IsNil)

	r.state.Unlock()
	err := configcore.Run(coreDev, tr)
	r.state.Lock()

	c.Check(err, ErrorMatches, `cannot set "core.experimental.not-graduated": unsupported system option`)
}

func warningsStrings(warnings []*state.Warning) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.String())
	}
	return messages
}
