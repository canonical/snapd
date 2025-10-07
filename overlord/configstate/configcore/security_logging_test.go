// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type securityLoggingSuite struct {
	configcoreSuite
}

var _ = Suite(&securityLoggingSuite{})

func (s *securityLoggingSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/systemd"), 0755)
	c.Assert(err, IsNil)

	s.AddCleanup(configcore.MockSeclogEnable(func() error { return nil }))
	s.AddCleanup(configcore.MockSeclogDisable(func() error { return nil }))
}

// Validation tests

func (s *securityLoggingSuite) TestValidateEnabledValid(c *C) {
	for _, val := range []string{"true", "false"} {
		err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf:  map[string]any{"security-logging.enabled": val},
		})
		c.Check(err, IsNil, Commentf("value %q", val))
	}
}

func (s *securityLoggingSuite) TestValidateEnabledInvalid(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]any{"security-logging.enabled": "maybe"},
	})
	c.Assert(err, ErrorMatches, `security-logging.enabled can only be set to 'true' or 'false'`)
}

func (s *securityLoggingSuite) TestValidateMaxSizeValid(c *C) {
	for _, val := range []string{"10M", "100M", "1G", "2T"} {
		err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]any{
				"security-logging.enabled":  "true",
				"security-logging.max-size": val,
			},
		})
		c.Check(err, IsNil, Commentf("value %q", val))
	}
}

func (s *securityLoggingSuite) TestValidateMaxSizeInvalidSuffix(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled":  "true",
			"security-logging.max-size": "100X",
		},
	})
	c.Assert(err, ErrorMatches, `security-logging.max-size cannot parse size "100X": must be a number followed by a suffix like K, M, G or T`)
}

func (s *securityLoggingSuite) TestValidateMaxSizeTooSmall(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled":  "true",
			"security-logging.max-size": "5M",
		},
	})
	c.Assert(err, ErrorMatches, `security-logging.max-size cannot set size "5M": must be at least 10M`)
}

func (s *securityLoggingSuite) TestValidateMaxSizeNonNumeric(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled":  "true",
			"security-logging.max-size": "abcM",
		},
	})
	c.Assert(err, ErrorMatches, `security-logging.max-size cannot parse size "abcM": must be a number followed by a suffix like K, M, G or T`)
}

func (s *securityLoggingSuite) TestValidateMaxSizeTooShort(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled":  "true",
			"security-logging.max-size": "M",
		},
	})
	c.Assert(err, ErrorMatches, `security-logging.max-size cannot parse size "M": must be a number followed by a suffix like K, M, G or T`)
}

// Nothing set -> no-op

func (s *securityLoggingSuite) TestHandleNothingSet(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]any{},
	})
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, HasLen, 0)
}

// Enable path (runtime)

func (s *securityLoggingSuite) TestEnableSecurityLogging(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled": "true",
		},
	})
	c.Assert(err, IsNil)

	// Check the journal conf was written with default max-size.
	confPath := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/journald@snapd-security.conf")
	c.Check(confPath, testutil.FileContains, "Storage=persistent")
	c.Check(confPath, testutil.FileContains, "SystemMaxUse=10M")
	c.Check(confPath, testutil.FileContains, "SyncIntervalSec=30s")
	c.Check(confPath, testutil.FileContains, "RateLimitBurst=10000")

	// Check systemctl calls: unmask, start, kill (reload config).
	// The mock systemd operates under a root dir, so --root is prepended.
	c.Assert(s.systemctlArgs, HasLen, 3)
	c.Check(s.systemctlArgs[0], testutil.DeepContains, "unmask")
	c.Check(s.systemctlArgs[0], testutil.DeepContains, "systemd-journald@snapd-security.socket")
	c.Check(s.systemctlArgs[1], testutil.DeepContains, "start")
	c.Check(s.systemctlArgs[1], testutil.DeepContains, "systemd-journald@snapd-security.socket")
	c.Check(s.systemctlArgs[2], testutil.DeepContains, "kill")
	c.Check(s.systemctlArgs[2], testutil.DeepContains, "systemd-journald@snapd-security.service")
}

func (s *securityLoggingSuite) TestEnableSecurityLoggingWithMaxSize(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled":  "true",
			"security-logging.max-size": "50M",
		},
	})
	c.Assert(err, IsNil)

	confPath := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/journald@snapd-security.conf")
	c.Check(confPath, testutil.FileContains, "SystemMaxUse=50M")
}

func (s *securityLoggingSuite) TestEnableCallsSeclogEnable(c *C) {
	enableCalled := false
	s.AddCleanup(configcore.MockSeclogEnable(func() error {
		enableCalled = true
		return nil
	}))

	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled": "true",
		},
	})
	c.Assert(err, IsNil)
	c.Check(enableCalled, Equals, true)
}

// Disable path (runtime)

func (s *securityLoggingSuite) TestDisableSecurityLogging(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled": "false",
		},
	})
	c.Assert(err, IsNil)

	// Check systemctl calls: mask the socket.
	c.Assert(s.systemctlArgs, HasLen, 1)
	c.Check(s.systemctlArgs[0], testutil.DeepContains, "mask")
	c.Check(s.systemctlArgs[0], testutil.DeepContains, "systemd-journald@snapd-security.socket")
}

func (s *securityLoggingSuite) TestDisableCallsSeclogDisable(c *C) {
	disableCalled := false
	s.AddCleanup(configcore.MockSeclogDisable(func() error {
		disableCalled = true
		return nil
	}))

	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.enabled": "false",
		},
	})
	c.Assert(err, IsNil)
	c.Check(disableCalled, Equals, true)
}

// Filesystem-only enable (preseeding/install)

func (s *securityLoggingSuite) TestEnableFSOnly(c *C) {
	rootDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(rootDir, "/etc/systemd"), 0755)
	c.Assert(err, IsNil)

	// Place a mask symlink as if previously disabled.
	maskDir := filepath.Join(rootDir, "/etc/systemd/system")
	err = os.MkdirAll(maskDir, 0755)
	c.Assert(err, IsNil)
	maskPath := filepath.Join(maskDir, "systemd-journald@snapd-security.socket")
	err = os.Symlink("/dev/null", maskPath)
	c.Assert(err, IsNil)

	err = configcore.FilesystemOnlyApply(coreDev, rootDir, map[string]any{
		"security-logging.enabled": "true",
	})
	c.Assert(err, IsNil)

	// Conf written.
	confPath := filepath.Join(rootDir, "/etc/systemd/journald@snapd-security.conf")
	c.Check(confPath, testutil.FileContains, "Storage=persistent")

	// Mask symlink removed.
	c.Check(osutil.IsSymlink(maskPath), Equals, false)

	// No systemctl calls in fs-only mode.
	c.Check(s.systemctlArgs, HasLen, 0)
}

// Filesystem-only disable (preseeding/install)

func (s *securityLoggingSuite) TestDisableFSOnly(c *C) {
	rootDir := c.MkDir()

	err := configcore.FilesystemOnlyApply(coreDev, rootDir, map[string]any{
		"security-logging.enabled": "false",
	})
	c.Assert(err, IsNil)

	// Mask symlink created.
	maskPath := filepath.Join(rootDir, "/etc/systemd/system", "systemd-journald@snapd-security.socket")
	c.Check(osutil.IsSymlink(maskPath), Equals, true)
	target, err := os.Readlink(maskPath)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "/dev/null")

	// No systemctl calls.
	c.Check(s.systemctlArgs, HasLen, 0)
}

// max-size alone implies enable

func (s *securityLoggingSuite) TestMaxSizeAloneImpliesEnable(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]any{
			"security-logging.max-size": "100M",
		},
	})
	c.Assert(err, IsNil)

	confPath := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/journald@snapd-security.conf")
	c.Check(confPath, testutil.FileContains, "SystemMaxUse=100M")

	// Systemctl was called (unmask, start, kill) — enable path was taken.
	c.Check(len(s.systemctlArgs) > 0, Equals, true)
}
