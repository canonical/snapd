// -*- Mode: Go; indent-tabs-mode: t -*-

package configcore_test

import (
	"encoding/json"
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
	content, _ := os.ReadFile(s.timesyncdConfigFile)
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
		// 0: Valid configuration with all keys, including json.Number, different units
		// and missing units
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"servers": []any{
						"192.168.15.1",
						"ntp.ubuntu.com",
					},
					"fallback-servers": []any{
						"192.168.1.100",
						"pool.ntp.org",
					},
					"root-distance-max-sec": json.Number(fmt.Sprint(5)),
					"poll-interval-min-sec": "30s",
					"poll-interval-max-sec": "40m",
					"connection-retry-sec":  "5s",
					"save-interval-sec":     "20",
				},
			},
			// Keep sorted in reverse order.
			// unit.Serialize does not serialize options in a predictable order, so
			// the test reads the file and sorts its lines. The reverse order makes
			// this look like a normal unit file
			expectedFileContent: []string{
				"[Time]",
				"SaveIntervalSec=20",
				"RootDistanceMaxSec=5s",
				"PollIntervalMinSec=30s",
				"PollIntervalMaxSec=40m",
				"NTP=192.168.15.1 ntp.ubuntu.com",
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
					"root-distance-max-sec": "5s",
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
			expectedError: "invalid NTP configuration: servers is an empty list",
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
			expectedError: "invalid NTP configuration: '\\*' is not a valid string",
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
					"root-distance-max-sec": "not-a-timespan",
				},
			},
			expectedError: "invalid NTP configuration: root-distance-max-sec: \"not-a-timespan\" is not a valid systemd.time timespan",
		},
		// 9: poll-interval-min-sec greater than poll-interval-max-sec
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"poll-interval-min-sec": "30m",
					"poll-interval-max-sec": "1m",
				},
			},
			expectedError: "invalid NTP configuration: poll-interval-min-sec \\(\"30m\"\\) cannot be greater than poll-interval-max-sec \\(\"1m\"\\)",
		},
		// 10: poll-interval-min-sec lower than minimum 16s
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"poll-interval-min-sec": "5s",
				},
			},
			expectedError: "invalid NTP configuration: poll-interval-min-sec: cannot be smaller than 16s",
		},
		// 11: connection-retry-sec lower than minimum 1s
		{
			newConfig: map[string]any{
				"system.ntp": map[string]any{
					"connection-retry-sec": "100ms",
				},
			},
			expectedError: "invalid NTP configuration: connection-retry-sec: cannot be smaller than 1s",
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
					"poll-interval-min-sec": 2.0,
				},
			},
			expectedError: "invalid NTP configuration: poll-interval-min-sec: invalid option type: float64",
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

func (s *ntpSuite) TestNTPSetSystemdAnalyzeError(c *C) {
	// Systemd-analyze returns the wrong format for the timespan value
	// It will return non-zero exit code if the analysis fails, so we need to mock it here to check
	// that we handle the format well and return the correct error message
	sysdAnalyzeCmdInvalidResponse := testutil.MockCommand(c, "systemd-analyze", "echo 'μs: xxxx'")
	defer sysdAnalyzeCmdInvalidResponse.Restore()

	// Apply the config
	invalidReponseConfig := configcore.PlainCoreConfig(map[string]any{
		"system.ntp": map[string]any{
			"connection-retry-sec": "xxxx",
		}})
	err := configcore.FilesystemOnlyRun(core24Dev, invalidReponseConfig)

	// Verify correct error is returned
	c.Check(err, ErrorMatches, "invalid NTP configuration: connection-retry-sec: \"xxxx\" is not a valid systemd.time timespan")
	s.verifyConfigfileContent(c, startingFileContent, "")
	c.Check(s.systemctlArgs, IsNil)

	// Return value that it too big for Int64 (9223372036854775807 + 1)
	sysdAnalyzeCmdBigNumber := testutil.MockCommand(c, "systemd-analyze", "echo 'μs: 9223372036854775808'")
	defer sysdAnalyzeCmdBigNumber.Restore()

	// Apply the config
	invalidNumberConfig := configcore.PlainCoreConfig(map[string]any{
		"system.ntp": map[string]any{
			"connection-retry-sec": "9223372036854s",
		}})
	err = configcore.FilesystemOnlyRun(core24Dev, invalidNumberConfig)

	// Verify correct error is returned
	c.Check(err, ErrorMatches, "invalid NTP configuration: connection-retry-sec: \"9223372036854s\" is not a valid systemd.time timespan")
	s.verifyConfigfileContent(c, startingFileContent, "")
	c.Check(s.systemctlArgs, IsNil)
}

// Test that setting a valid configuration fails when the systemd folder cannot be accessed due to
// missing write permissions
func (s *ntpSuite) TestNTPSetCannotCreateSystemdFolder(c *C) {
	etcFolder := filepath.Join(dirs.GlobalRootDir, "etc")
	systemdConfigFolder := filepath.Join(etcFolder, "systemd")
	systemdConfigFolderAlternateName := filepath.Join(etcFolder, "systemd.bak")
	// Instead of removing it, we rename it to avoid issues for other files
	os.Rename(systemdConfigFolder, systemdConfigFolderAlternateName)
	// Remove write permission for /etc so that /etc/systemd cannot be recreated
	c.Assert(os.Chmod(etcFolder, 0111), IsNil)
	defer os.Chmod(etcFolder, 0755)
	defer os.Rename(systemdConfigFolderAlternateName, systemdConfigFolder)

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

func (s *ntpSuite) TestNTPSetErrorReadingDiskConfiguration(c *C) {
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
		"save-interval-sec": "10s",
	})
}

func (s *ntpSuite) TestNTPConfigurationDeepEqual(c *C) {
	// Test identical configurations are equal
	config1 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"root-distance-max-sec": "5s",
	}
	config2 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"root-distance-max-sec": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config2), Equals, true)

	// Test different server lists are not equal
	config3 := map[string]any{
		"servers": []any{
			"192.168.1.2",
			"ntp.ubuntu.com",
		},
		"root-distance-max-sec": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config3), Equals, false)

	// Test different timespan values are not equal
	config4 := map[string]any{
		"servers": []any{
			"192.168.1.1",
			"ntp.ubuntu.com",
		},
		"root-distance-max-sec": "10s",
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
		"root-distance-max-sec": "5s",
		"poll-interval-min-sec": "16s",
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
		"root-distance-max-sec": "5s",
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

	// Test configuration with removed fields
	config9 := map[string]any{
		"poll-interval-min-sec": "16s",
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
		"root-distance-max-sec": "5s",
	}
	c.Check(configcore.NTPConfigurationDeepEqual(config1, config10), Equals, false)
	c.Check(configcore.NTPConfigurationDeepEqual(config10, config1), Equals, false)
}
