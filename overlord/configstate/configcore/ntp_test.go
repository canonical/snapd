// -*- Mode: Go; indent-tabs-mode: t -*-

package configcore_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type ntpSuite struct {
	configcoreSuite
	timesyncdConfigFile string
	sysdLog             [][]string
}

var (
	_                   = Suite(&ntpSuite{})
	startingFileContent = []string{
		"[Time]",
		"RootDistanceMaxSec=5s",
		"NTP=ntp.ubuntu.com",
	}
	validConfigurationExample = map[string]any{
		"system.ntp": map[string]any{
			"servers": []any{
				"192.168.15.1",
				"ntp.ubuntu.com",
			},
		},
	}
	configurationExampleFileContent = []string{
		"[Time]",
		"NTP=192.168.15.1 ntp.ubuntu.com",
	}
)

func (s *ntpSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc/systemd"), 0755), IsNil)

	// Create the config file and write a predictable configuration
	systemdConfigFolder := filepath.Join(dirs.GlobalRootDir, "etc/systemd")
	err := os.MkdirAll(systemdConfigFolder, 0755)
	c.Assert(err, IsNil)
	s.timesyncdConfigFile = filepath.Join(systemdConfigFolder, "timesyncd.conf")
	err = os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	c.Assert(err, IsNil)

	// We are on Core
	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)
}

func (s *ntpSuite) writeExampleConfigFile(c *C) {
	err := os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	c.Assert(err, IsNil)
}

func (s *ntpSuite) verifyConfigfileContent(c *C, expectedContent []string, comment string) {
	// The serialization order of the individual options is not predictable. Read the file, sort
	// the lines and compare them against the (already sorted) expected content.
	content, err := os.ReadFile(s.timesyncdConfigFile)
	c.Assert(err, IsNil)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	// Sort in reverse order so the output looks "normal" with the "[Time]" line at the top
	sort.Sort(sort.Reverse(sort.StringSlice(lines)))

	if len(expectedContent) != 0 {
		c.Check([]string(lines), DeepEquals, expectedContent, Commentf("%v", comment))
	} else {
		c.Check([]string(lines), DeepEquals, startingFileContent, Commentf("%v", comment))
	}
	// Reset configuration file to the default value for the next test
	s.writeExampleConfigFile(c)
}

// Test setting various configurations, with multiple valid and invalid configurations
func (s *ntpSuite) TestNTPSetValidateValues(c *C) {
	var getConfigurationTests = []struct {
		newConfig            map[string]any
		expectedFileDeleted  bool
		expectedFileContent  []string
		expectServiceRestart bool
		expectedError        string
	}{
		// 0: Valid configuration with all keys and different Go duration units.
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						"192.168.15.1",
						"ntp.ubuntu.com",
						"2610:20:6f15:15::25", // time-f-g.nist.gov
					},
					"fallback-servers": []any{
						"192.168.1.100",
						"pool.ntp.org",
					},
					"max-root-time-distance":    "5s",
					"min-poll-interval":         "30s",
					"max-poll-interval":         "40m0s",
					"connection-retry-interval": "5s",
					"save-interval":             "20s",
				},
			},
			// Keep sorted in reverse order.
			// unit.Serialize does not serialize options in a predictable order, so
			// the test reads the file and sorts its lines. The reverse order makes
			// this look like a normal unit file
			expectedFileContent: []string{
				"[Time]",
				"SaveIntervalSec=20s",
				"RootDistanceMaxSec=5s",
				"PollIntervalMinSec=30s",
				"PollIntervalMaxSec=40m0s",
				"NTP=192.168.15.1 ntp.ubuntu.com 2610:20:6f15:15::25",
				"FallbackNTP=192.168.1.100 pool.ntp.org",
				"ConnectionRetrySec=5s",
			},
			expectServiceRestart: true,
			expectedError:        "",
		},
		// 1: Valid empty configuration
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{},
			},
			expectedFileDeleted:  true,
			expectServiceRestart: true,
			expectedError:        "",
		},
		// 2: Valid configuration identical to current one
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						"ntp.ubuntu.com",
					},
					"max-root-time-distance": "5s",
				},
			},
			expectServiceRestart: false,
			expectedFileContent:  startingFileContent,
		},
		// 3: Invalid format for server list
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": "192.168.15.1",
				},
			},
			expectedError: "invalid NTP configuration: servers is not a list of server names",
		},
		// 4: Empty server list
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{},
				},
			},
			expectServiceRestart: true,
			expectedFileContent: []string{
				"[Time]",
				"NTP=",
			},
		},
		// 5: Wrong value type in server list
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						42,
					},
				},
			},
			expectedError: "invalid NTP configuration: server 42 is not a valid string",
		},
		// 6: Malformed IP address in server list
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						"192.168.1....",
					},
				},
			},
			expectedError: "invalid NTP configuration: \"192.168.1....\" is not a valid server name",
		},
		// 7: Malformed domain name in server list
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						"canonical......com",
					},
				},
			},
			expectedError: "invalid NTP configuration: \"canonical......com\" is not a valid server name",
		},
		// 8: Invalid configuration with wrong systemd timespans
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"max-root-time-distance": "not-a-timespan",
				},
			},
			expectedError: "invalid NTP configuration: max-root-time-distance: time: invalid duration \"not-a-timespan\"",
		},
		// 9: min-poll-interval greater than max-poll-interval
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"min-poll-interval": "30m",
					"max-poll-interval": "1m",
				},
			},
			expectedError: "invalid NTP configuration: min-poll-interval \\(\"30m\"\\) cannot be greater than max-poll-interval \\(\"1m\"\\)",
		},
		// 10: min-poll-interval lower than minimum 16s
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"min-poll-interval": "5s",
				},
			},
			expectedError: "invalid NTP configuration: min-poll-interval: cannot be smaller than 16s",
		},
		// 11: connection-retry-interval lower than minimum 1s
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"connection-retry-interval": "100ms",
				},
			},
			expectedError: "invalid NTP configuration: connection-retry-interval: cannot be smaller than 1s",
		},
		// 12: unsupported configuration option
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"new-option": "new-value",
				},
			},
			expectedError: "invalid NTP configuration: unsupported configuration option \"new-option\"",
		},
		// 13: invalid option type for a timespan option
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"min-poll-interval": 2.0,
				},
			},
			expectedError: "invalid NTP configuration: min-poll-interval is not a string",
		},
		// 14: missing time unit for a timespan option
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"min-poll-interval": "2",
				},
			},
			expectedError: "invalid NTP configuration: min-poll-interval: time: missing unit in duration \"2\"",
		},
		// 15: sub-microsecond duration (below systemd's resolution)
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"max-root-time-distance": "500ns",
				},
			},
			expectedError: `invalid NTP configuration: max-root-time-distance: duration "500ns" is below systemd's minimum resolution of 1µs`,
		},
	}

	for i, test := range getConfigurationTests {
		// Apply the config
		conf := configcore.PlainCoreConfig(test.newConfig)
		err := configcore.FilesystemOnlyRun(core24Dev, conf)

		// Verify correct error is returned
		if test.expectedError == "" {
			c.Check(err, IsNil, Commentf("config validation: %v", i))
		} else {
			c.Check(err, ErrorMatches, test.expectedError, Commentf("config validation: %v", i))
		}

		// The configuration file content is either the new configuration if it was valid, or
		// the original configuration if the new one was invalid
		// If the configuration is empty, the file should be deleted
		if test.expectedFileDeleted {
			_, err = os.Lstat(s.timesyncdConfigFile)
			c.Check(os.IsNotExist(err), Equals, true, Commentf("config validation: %v", i))
		} else {
			s.verifyConfigfileContent(c, test.expectedFileContent, fmt.Sprintf("config validation: %v", i))
		}
		s.writeExampleConfigFile(c)

		// Check that the timesyncd service was restarted only if necessary, i.e. the new
		// configuration was valid and different from the original one
		if test.expectServiceRestart {
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"reload-or-restart", "systemd-timesyncd.service"},
			}, Commentf("config validation: %v", i))
		} else {
			c.Check(s.systemctlArgs, IsNil, Commentf("config validation: %v", i))
		}
		s.systemctlArgs = nil
	}
}

// Test that setting a valid configuration fails when the systemd folder cannot be accessed due to
// missing write permissions
func (s *ntpSuite) TestNTPSetCannotCreateSystemdFolder(c *C) {
	etcFolder := filepath.Join(dirs.GlobalRootDir, "etc")
	systemdConfigFolder := filepath.Join(etcFolder, "systemd")
	systemdConfigFolderAlternateName := filepath.Join(etcFolder, "systemd.bak")
	// Instead of removing it, we rename it to avoid issues for other files
	c.Assert(os.Rename(systemdConfigFolder, systemdConfigFolderAlternateName), IsNil)
	// Remove write permission for /etc so that /etc/systemd cannot be recreated
	c.Assert(os.Chmod(etcFolder, 0111), IsNil)
	defer func() { c.Assert(os.Rename(systemdConfigFolderAlternateName, systemdConfigFolder), IsNil) }()
	defer os.Chmod(etcFolder, 0755)

	conf := configcore.PlainCoreConfig(validConfigurationExample)

	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Assert(err, ErrorMatches, "mkdir /tmp/check-.*/etc/systemd: permission denied")
}

// Test that setting a valid configuration fails when the systemd folder cannot be accessed due to
// missing write permissions
func (s *ntpSuite) TestNTPSetValidConfigurationMissingFolderPermissions(c *C) {
	systemdConfigFolder := filepath.Join(dirs.GlobalRootDir, "etc/systemd")
	c.Assert(os.Chmod(systemdConfigFolder, 0111), IsNil)
	defer os.Chmod(systemdConfigFolder, 0755)

	conf := configcore.PlainCoreConfig(validConfigurationExample)

	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Assert(err, ErrorMatches, "cannot write NTP configuration: open .*/etc/systemd/timesyncd.conf.*: permission denied")
	s.verifyConfigfileContent(c, startingFileContent, "")
}

func (s *ntpSuite) TestNTPSetErrorRemoveFileEmptyConfiguration(c *C) {
	// Change /etc/systemd permissions to inhibit file deletion when the configuration is empty
	systemdConfigFolder := filepath.Join(dirs.GlobalRootDir, "etc/systemd")
	c.Assert(os.Chmod(systemdConfigFolder, 0111), IsNil)
	defer os.Chmod(systemdConfigFolder, 0755)

	conf := configcore.PlainCoreConfig(map[string]any{
		"system.ntp": map[string]any{},
	})

	// The config file not being readable triggers an error and the configuration
	// is not updated
	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Assert(err, ErrorMatches, "cannot reset NTP configuration to defaults: remove .*/etc/systemd/timesyncd.conf: permission denied")
}

func (s *ntpSuite) TestNTPSetErrorReadingDiskConfiguration(c *C) {
	// Change file permissions to inhibit reading it
	os.Chmod(s.timesyncdConfigFile, 0000)
	defer os.Chmod(s.timesyncdConfigFile, 0644)

	conf := configcore.PlainCoreConfig(validConfigurationExample)

	// The config file not being readable triggers an error and the configuration
	// is not updated
	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Assert(err, ErrorMatches, "cannot read NTP configuration file /etc/systemd/timesyncd.conf: open .*/etc/systemd/timesyncd.conf: permission denied")
}

func (s *ntpSuite) TestNTPSetMissingConfigFile(c *C) {
	// Remove config file
	os.Remove(s.timesyncdConfigFile)

	conf := configcore.PlainCoreConfig(validConfigurationExample)

	// The config file not being present is not an error and the configuration is
	// set successfully
	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Assert(err, IsNil)
	s.verifyConfigfileContent(c, configurationExampleFileContent, "")
}

func (s *ntpSuite) TestNTPSetRestartDaemonError(c *C) {
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return nil, fmt.Errorf("boom")
	})
	defer r()

	conf := configcore.PlainCoreConfig(validConfigurationExample)

	err := configcore.FilesystemOnlyRun(core24Dev, conf)
	c.Check(err, ErrorMatches, "cannot restart timesyncd daemon after configuration change: boom")
}

func (s *ntpSuite) TestNTPGetMissingConfigFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Remove config file
	os.Remove(s.timesyncdConfigFile)

	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "snap \"core\" has no \"system.ntp\" configuration option")
	c.Check(ntpConfig, DeepEquals, map[string]any(nil))
}

func (s *ntpSuite) TestNTPGetErrorOpeningFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Change file permissions to inhibit reading it
	os.Chmod(s.timesyncdConfigFile, 0000)
	defer os.Chmod(s.timesyncdConfigFile, 0644)

	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot read NTP configuration file /etc/systemd/timesyncd.conf: open .*/etc/systemd/timesyncd.conf: permission denied")
}

func (s *ntpSuite) TestNTPGetInvalidSystemdUnit(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Write corrupted unit
	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[invalid-unit"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot parse systemd unit in configuration file /etc/systemd/timesyncd.conf: unable to find end of section")
}

func (s *ntpSuite) TestNTPGetEmptySystemdUnit(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Write unit with no options
	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "snap \"core\" has no \"system.ntp\" configuration option")
}

func (s *ntpSuite) TestNTPGetUnsupportedOption(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// The unsupported option is skipped and does not cause an error, but the rest of the configuration is still read correctly
	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nUnsupportedOption=1\nSaveIntervalSec=10s"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, IsNil)
	c.Assert(ntpConfig, DeepEquals, map[string]any{
		"save-interval": "10s",
	})
}

func (s *ntpSuite) TestNTPGetSystemdDurationsAsGoDurations(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nRootDistanceMaxSec=1.5h\nSaveIntervalSec=40min"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, IsNil)
	c.Assert(ntpConfig, DeepEquals, map[string]any{
		"max-root-time-distance": "1h30m0s",
		"save-interval":          "40m0s",
	})
}

func (s *ntpSuite) TestNTPGetInvalidSystemdDuration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nRootDistanceMaxSec=not-a-timespan"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot parse NTP option: max-root-time-distance: \"not-a-timespan\" is not a valid systemd.time timespan")
}

func (s *ntpSuite) TestNTPGetSystemdAnalyzeNoTimespanLine(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// systemd-analyze exits successfully but its output does not contain a
	// "µs:"/"us:" line, so no timespan can be extracted.
	sysdAnalyzeCmd := testutil.MockCommand(c, "systemd-analyze", "echo 'no timespan here'")
	defer sysdAnalyzeCmd.Restore()

	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nRootDistanceMaxSec=5s"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot parse NTP option: max-root-time-distance: \"5s\" is not a valid systemd.time timespan")
}

func (s *ntpSuite) TestNTPGetSystemdAnalyzeTimespanOverflowsInt64(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// The captured µs value does not fit into an int64 (math.MaxInt64 + 1),
	// so strconv.ParseInt fails.
	sysdAnalyzeCmd := testutil.MockCommand(c, "systemd-analyze", "echo 'μs: 9223372036854775808'")
	defer sysdAnalyzeCmd.Restore()

	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nRootDistanceMaxSec=5s"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot parse NTP option: max-root-time-distance: \"5s\" is not a valid systemd.time timespan")
}

func (s *ntpSuite) TestNTPGetSystemdAnalyzeTimespanTooLargeForGoDuration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// The captured µs value fits into an int64 but exceeds the maximum number of
	// microseconds representable by a time.Duration (math.MaxInt64 microseconds).
	sysdAnalyzeCmd := testutil.MockCommand(c, "systemd-analyze", "echo 'μs: 9223372036854775807'")
	defer sysdAnalyzeCmd.Restore()

	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nRootDistanceMaxSec=5s"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, ErrorMatches, "cannot parse NTP option: max-root-time-distance: \"5s\" is too large for a Go duration")
}

func (s *ntpSuite) TestNTPGetEmptyServerList(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// The empty server list is read as an empty list and not an as an error
	c.Assert(os.WriteFile(s.timesyncdConfigFile, []byte("[Time]\nNTP=         \nSaveIntervalSec=10s"), 0644), IsNil)
	defer os.WriteFile(s.timesyncdConfigFile, []byte(strings.Join(startingFileContent, "\n")), 0644)
	tr := config.NewTransaction(s.state)

	var ntpConfig map[string]any
	err := tr.Get("core", "system.ntp", &ntpConfig)
	c.Assert(err, IsNil)
	c.Assert(ntpConfig, DeepEquals, map[string]any{
		"save-interval": "10s",
		"servers":       []any{},
	})
}

func (s *ntpSuite) TestNTPConfigurationDeepEqual(c *C) {
	// Test identical configurations are equal
	config1 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "5s",
	}
	config2 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config2), Equals, true)

	// Test different server lists are not equal
	config3 := map[string]any{
		"servers": []any{
			"192.168.1.2",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config3), Equals, false)

	// Test different timespan values are not equal
	config4 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "10s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config4), Equals, false)

	// Test empty configurations are equal
	config5 := map[string]any{}
	c.Check(configcore.NTPConfigurationDeepEqual(config5, config5), Equals, true)

	// Test configuration with additional fields
	config6 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "5s",
		"min-poll-interval":      "16s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config6), Equals, false)

	// Test server list as []string result of parsing timesyncd.conf (oldConfig), instead of
	// []any coming from tr.Get call (newConfig). The slices should be considered equal even
	// if their type is different, as long as their content is the same.
	config7 := map[string]any{
		"servers": []string{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"max-root-time-distance": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config7), Equals, true)

	// Test identical "nil" configurations are equal
	var config8 map[string]any
	c.Check(configcore.NTPConfigurationDeepEqual(config8, config8), Equals, true)

	// Test "nil" configuration is "equal" to empty configuration
	c.Check(configcore.NTPConfigurationDeepEqual(config8, config5), Equals, true)

	// Test "nil" and empty configurations are different from any configuration
	c.Check(configcore.NTPConfigurationDeepEqual(config8, config1), Equals, false)
	c.Check(configcore.NTPConfigurationDeepEqual(config5, config1), Equals, false)

	// Test configuration with differently named field (max to min)
	config9 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"root-distance-min": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config9), Equals, false)

	// Test configuration with wrong types for values
	// Check both as first and second argument to hit both branches of the function
	config10 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
			12.5,
		},
		"max-root-time-distance": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config10), Equals, false)
	c.Check(configcore.NTPConfigurationDeepEqual(config10, config1), Equals, false)

	// Test that equivalent duration representations are considered equal.
	// The read path normalises disk values (e.g. "40min" → "40m0s"), while the
	// user may provide the same duration as "40m". Both should be equal to avoid
	// an unnecessary timesyncd restart.
	config11 := map[string]any{
		"max-root-time-distance": "40m0s", // normalised form, as read from disk
	}
	config12 := map[string]any{
		"max-root-time-distance": "40m", // user-supplied shorthand
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config11, config12), Equals, true)
	c.Check(configcore.NTPConfigurationDeepEqual(config12, config11), Equals, true)
}
