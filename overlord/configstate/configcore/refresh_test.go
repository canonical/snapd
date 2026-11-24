// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type refreshSuite struct {
	configcoreSuite
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) TestConfigureRefreshDeltaFormats(c *C) {
	for _, tc := range []struct {
		value any
		want  []string
		err   string
	}{
		{nil, nil, ""},
		{[]string{"xdelta3"}, []string{"xdelta3"}, ""},
		{[]string{"snap-1-1-xdelta3", "xdelta3"}, []string{"snap-1-1-xdelta3", "xdelta3"}, ""},
		{[]string{}, nil, "refresh.delta-formats must contain at least one delta format"},
		{[]string{"unknown"}, nil, `unsupported delta format "unknown" in refresh.delta-formats`},
		{[]string{"xdelta3", "xdelta3"}, nil, `duplicate delta format "xdelta3" in refresh.delta-formats`},
		{"xdelta3", nil, ".*cannot unmarshal string.*"},
		{[]any{"xdelta3", false}, nil, ".*cannot unmarshal bool.*"},
	} {
		s.state.Lock()
		tr := config.NewTransaction(s.state)
		c.Assert(tr.Set("core", "refresh.delta-formats", tc.value), IsNil)
		s.state.Unlock()
		err := configcore.Run(classicDev, configcore.NewRunTransaction(tr, nil))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
			s.state.Lock()
			tr.Commit()
			var formats []string
			err := config.NewTransaction(s.state).Get("core", "refresh.delta-formats", &formats)
			s.state.Unlock()
			if tc.want == nil {
				c.Check(config.IsNoOption(err), Equals, true)
			} else {
				c.Assert(err, IsNil)
			}
			c.Check(formats, DeepEquals, tc.want)
		}
	}
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormats(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, tc := range []struct {
		flag any
		want []string
	}{
		{true, []string{"snap-1-1-xdelta3", "xdelta3"}},
		{"true", []string{"snap-1-1-xdelta3", "xdelta3"}},
		{false, []string{"xdelta3"}},
		{"false", []string{"xdelta3"}},
		{nil, nil},
		{"", nil},
	} {
		tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		c.Assert(tr.Set("core", "experimental.snap-delta-format", tc.flag), IsNil)
		c.Assert(configcore.MigrateRefreshDeltaFormats(tr), IsNil)
		var formats []string
		err := tr.Get("core", "refresh.delta-formats", &formats)
		if tc.want == nil {
			c.Check(config.IsNoOption(err), Equals, true)
		} else {
			c.Assert(err, IsNil)
		}
		c.Check(formats, DeepEquals, tc.want)
		var flag any
		c.Assert(tr.GetMaybe("core", "experimental.snap-delta-format", &flag), IsNil)
		c.Check(flag, DeepEquals, tc.flag)
	}
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormatsPreservesConfiguredValue(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	for _, value := range []any{[]string{"xdelta3"}, []string{"future-format"}, "invalid"} {
		tr := config.NewTransaction(s.state)
		c.Assert(tr.Set("core", "experimental.snap-delta-format", true), IsNil)
		c.Assert(tr.Set("core", "refresh.delta-formats", value), IsNil)
		tr.Commit()
		runTr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		c.Assert(configcore.MigrateRefreshDeltaFormats(runTr), IsNil)
		c.Check(runTr.Changes(), HasLen, 0)
		var before, after any
		c.Assert(tr.Get("core", "refresh.delta-formats", &before), IsNil)
		c.Assert(runTr.Get("core", "refresh.delta-formats", &after), IsNil)
		c.Check(after, DeepEquals, before)
	}
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormatsInvalidFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	for _, flag := range []any{"invalid", 1, []string{"true"}, map[string]bool{"enabled": true}} {
		tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		c.Assert(tr.Set("core", "experimental.snap-delta-format", flag), IsNil)
		log, restore := logger.MockLogger()
		err := configcore.MigrateRefreshDeltaFormats(tr)
		restore()
		c.Assert(err, IsNil)
		c.Check(log.String(), testutil.Contains, "cannot migrate invalid experimental.snap-delta-format value")
		var formats any
		c.Check(config.IsNoOption(tr.Get("core", "refresh.delta-formats", &formats)), Equals, true)
	}
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormatsSkippedWhenComplete(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("refresh-delta-formats-migrated", true)
	tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	c.Assert(tr.Set("core", "experimental.snap-delta-format", true), IsNil)
	c.Assert(configcore.MigrateRefreshDeltaFormats(tr), IsNil)
	var formats any
	c.Check(config.IsNoOption(tr.Get("core", "refresh.delta-formats", &formats)), Equals, true)

	s.state.Unlock()
	err := configcore.Run(classicDev, tr)
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Check(config.IsNoOption(tr.Get("core", "refresh.delta-formats", &formats)), Equals, true)
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormatsOnFlagChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	for _, tc := range []struct {
		flag     bool
		explicit []string
		want     []string
	}{
		{true, nil, []string{"snap-1-1-xdelta3", "xdelta3"}},
		{false, nil, []string{"xdelta3"}},
		{true, []string{"xdelta3"}, []string{"xdelta3"}},
	} {
		tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		c.Assert(tr.Set("core", "experimental.snap-delta-format", tc.flag), IsNil)
		if tc.explicit != nil {
			c.Assert(tr.Set("core", "refresh.delta-formats", tc.explicit), IsNil)
		}
		s.state.Unlock()
		err := configcore.Run(classicDev, tr)
		s.state.Lock()
		c.Assert(err, IsNil)
		var formats []string
		c.Assert(tr.Get("core", "refresh.delta-formats", &formats), IsNil)
		c.Check(formats, DeepEquals, tc.want)
	}
}

func (s *refreshSuite) TestMigrateRefreshDeltaFormatsKeepsExplicitUnset(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	setupTr := config.NewTransaction(s.state)
	c.Assert(setupTr.Set("core", "refresh.delta-formats", []string{"xdelta3"}), IsNil)
	setupTr.Commit()
	tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
	c.Assert(tr.Set("core", "experimental.snap-delta-format", true), IsNil)
	c.Assert(tr.Set("core", "refresh.delta-formats", nil), IsNil)
	s.state.Unlock()
	err := configcore.Run(classicDev, tr)
	s.state.Lock()
	c.Assert(err, IsNil)
	var formats any
	c.Check(config.IsNoOption(tr.Get("core", "refresh.delta-formats", &formats)), Equals, true)
}

func (s *refreshSuite) TestConfigureRefreshDeltaFormatsIgnoresStoredInvalidValue(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	for _, value := range []any{[]string{"future-format"}, "invalid"} {
		tr := config.NewTransaction(s.state)
		c.Assert(tr.Set("core", "refresh.delta-formats", value), IsNil)
		tr.Commit()
		runTr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
		c.Assert(runTr.Set("core", "refresh.rate-limit", "1MB"), IsNil)
		s.state.Unlock()
		err := configcore.Run(classicDev, runTr)
		s.state.Lock()
		c.Assert(err, IsNil)
	}
}

func (s *refreshSuite) TestEarlyRefreshDeltaFormatsLegacyFlag(c *C) {
	for _, tc := range []struct {
		flag  any
		value any
		want  []string
		err   string
	}{
		{true, nil, []string{"snap-1-1-xdelta3", "xdelta3"}, ""},
		{true, []string{"xdelta3"}, []string{"xdelta3"}, ""},
		{nil, []string{"xdelta3"}, []string{"xdelta3"}, ""},
		{nil, []string{"unknown"}, nil, `unsupported delta format "unknown" in refresh.delta-formats`},
		{true, []string{"xdelta3", "xdelta3"}, nil, `duplicate delta format "xdelta3" in refresh.delta-formats`},
		{nil, "xdelta3", nil, ".*cannot unmarshal string.*"},
	} {
		s.state.Lock()
		tr := config.NewTransaction(s.state)
		s.state.Unlock()
		values := make(map[string]any)
		if tc.flag != nil {
			values["experimental.snap-delta-format"] = tc.flag
		}
		if tc.value != nil {
			values["refresh.delta-formats"] = tc.value
		}
		err := configcore.Early(classicDev, configcore.NewRunTransaction(tr, nil), values)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
			continue
		}
		c.Assert(err, IsNil)
		s.state.Lock()
		var formats []string
		err = tr.Get("core", "refresh.delta-formats", &formats)
		s.state.Unlock()
		c.Assert(err, IsNil)
		c.Check(formats, DeepEquals, tc.want)
	}
}

func (s *refreshSuite) TestConfigureRefreshTimerHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.timer": "8:00~12:00/2",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshTimerRejected(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.timer": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": "invalid" is not a valid weekday`)
}

func (s *refreshSuite) TestConfigureRefreshTimerManagedIgnored(c *C) {
	for _, opt := range []string{"refresh.timer", "refresh.schedule"} {
		cfg := &mockConf{
			state: s.state,
			// invalid value present in the state
			conf: map[string]any{
				opt: "managed",
			},
		}
		s.state.Lock()
		s.state.OkayWarnings(time.Now())
		s.state.Unlock()
		err := configcore.Run(classicDev, cfg)
		c.Assert(err, IsNil)

		s.state.Lock()
		c.Check(cfg.conf[opt], Equals, "managed")
		s.state.Unlock()
	}
}

func (s *refreshSuite) TestConfigureRefreshTimerManagedChangeError(c *C) {
	for _, opt := range []string{"refresh.timer", "refresh.schedule"} {
		cfg := &mockConf{
			state: s.state,
			conf: map[string]any{
				// valid value in the state, should remain intact
				opt: "fri",
			},
			changes: map[string]any{
				opt: "managed",
			},
		}
		err := configcore.Run(classicDev, cfg)
		c.Assert(err, ErrorMatches, `cannot set schedule to managed`)
		// old value still present
		c.Check(cfg.conf[opt], Equals, "fri")
	}
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.schedule": "8:00-12:00",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleRejected(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.schedule": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": not a valid interval`)

	// check that refresh.schedule is verified against legacy parser
	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.schedule": "8:00~12:00/2",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "8:00~12:00": not a valid interval`)
}

func (s *refreshSuite) TestConfigureRefreshHoldHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.hold": "2018-08-18T15:00:00Z",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldForeverHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.hold": "forever",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.hold": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.hold cannot be parsed:.*`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.metered": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.metered value "invalid" is invalid`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.metered": "hold",
		},
	})
	c.Assert(err, IsNil)

	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.metered": "",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshRetainHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.retain": "4",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshRetainUnderRange(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.retain": "1",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "1"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainOverRange(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.retain": "100",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "100"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"refresh.retain": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "invalid"`)
}

func (s *refreshSuite) TestConfigureRefreshMaxInhibitionDays(c *C) {
	data := []struct {
		val any
		err string
	}{
		{val: "zzz", err: `max-inhibition-days must be a number between 1 and 21, not "zzz"`},
		{val: "-1", err: `max-inhibition-days must be a number between 1 and 21, not "-1"`},
		{val: -1, err: `max-inhibition-days must be a number between 1 and 21, not "-1"`},
		{val: "23", err: `max-inhibition-days must be a number between 1 and 21, not "23"`},
		{val: 0, err: `max-inhibition-days must be a number between 1 and 21, not "0"`},
		// happy cases
		{val: nil}, // default value (14 days)
		{val: ""},  // default value (14 days)
		{val: 1},
		{val: "1"},
		{val: 14},
	}
	for _, tc := range data {
		err := configcore.Run(classicDev, &mockConf{
			state: s.state,
			conf: map[string]any{
				"refresh.max-inhibition-days": tc.val,
			},
		})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}
