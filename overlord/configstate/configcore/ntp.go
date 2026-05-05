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

package configcore

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/coreos/go-systemd/unit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.ntp.servers"] = true
	supportedConfigurations["core.system.ntp.fallback-servers"] = true
	supportedConfigurations["core.system.ntp.root-distance-max-sec"] = true
	supportedConfigurations["core.system.ntp.poll-interval-min-sec"] = true
	supportedConfigurations["core.system.ntp.poll-interval-max-sec"] = true
	supportedConfigurations["core.system.ntp.connection-retry-sec"] = true
	supportedConfigurations["core.system.ntp.save-interval-sec"] = true
	// and register it as a external config
	config.RegisterExternalConfig("core", "system.ntp", getNTPFromSystemHelper)
}

// Too simplistic?
var validHostname = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9]))*$`).MatchString
var validIPv4 = net.ParseIP

var timesyncdToSnapKeyMapping = map[string]string{
	"NTP":                "servers",
	"FallbackNTP":        "fallback-servers",
	"RootDistanceMaxSec": "root-distance-max-sec",
	"PollIntervalMinSec": "poll-interval-min-sec",
	"PollIntervalMaxSec": "poll-interval-max-sec",
	"ConnectionRetrySec": "connection-retry-sec",
	"SaveIntervalSec":    "save-interval-sec",
}

var snapToTimesyncdKeyMapping = map[string]string{
	"servers":               "NTP",
	"fallback-servers":      "FallbackNTP",
	"root-distance-max-sec": "RootDistanceMaxSec",
	"poll-interval-min-sec": "PollIntervalMinSec",
	"poll-interval-max-sec": "PollIntervalMaxSec",
	"connection-retry-sec":  "ConnectionRetrySec",
	"save-interval-sec":     "SaveIntervalSec",
}

func validateNTPSettings(tr ConfGetter) error {
	var ntpCfg map[string]any
	if err := tr.Get("core", "system.ntp", &ntpCfg); err != nil && !config.IsNoOption(err) {
		return fmt.Errorf("cannot get NTP config: %v", err)
	}

	for k, v := range ntpCfg {
		if err := validateSingleNTPSetting(k, v); err != nil {
			return fmt.Errorf("invalid NTP configuration: %v", err)
		}
	}

	// Validate that poll-interval-min-sec > poll-interval-min-sec
	// Use the systemd defaults if they have not been overwritten by the user
	pollIntervalMinSecString := "32s"
	pollIntervalMaxSecString := "2048s"
	customPollInterval := false
	// Validation for user submitted values has already been done
	if minSec, exists := ntpCfg["poll-interval-min-sec"]; exists {
		pollIntervalMinSecString, _ = mapSnapOptionValueToUnitOptionValue(minSec)
		customPollInterval = true
	}
	if maxSec, exists := ntpCfg["poll-interval-max-sec"]; exists {
		pollIntervalMaxSecString, _ = mapSnapOptionValueToUnitOptionValue(maxSec)
		customPollInterval = true
	}

	if customPollInterval {
		pollIntervalMinUSec, _ := convertSystemdTimespanToUs(pollIntervalMinSecString)
		pollIntervalMaxUSec, _ := convertSystemdTimespanToUs(pollIntervalMaxSecString)

		if pollIntervalMinUSec > pollIntervalMaxUSec {
			return fmt.Errorf("invalid NTP configuration: poll-interval-min-sec (%q) cannot be greater than poll-interval-max-sec (%q)", pollIntervalMinSecString, pollIntervalMaxSecString)
		}
	}

	return nil
}

func validateSingleNTPSetting(key string, value any) (err error) {
	switch key {
	case "servers", "fallback-servers":
		servers, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%v is not a list of server names", key)
		}
		if len(servers) == 0 {
			return fmt.Errorf("%v is an empty list", key)
		}

		if err := validateNTPServers(servers); err != nil {
			return err
		}
		return nil

	case "root-distance-max-sec", "poll-interval-min-sec", "poll-interval-max-sec", "connection-retry-sec", "save-interval-sec":
		span, err := mapSnapOptionValueToUnitOptionValue(value)
		if err != nil {
			return fmt.Errorf("%v: %v", key, err)
		}

		timespanUs, err := validateSystemdTimeSpanFormat(span)
		if err != nil {
			return fmt.Errorf("%v: %v", key, err)
		}

		if key == "poll-interval-min-sec" && timespanUs < 16000000 {
			return fmt.Errorf("poll-interval-min-sec: cannot be smaller than 16s.")
		}
		if key == "connection-retry-sec" && timespanUs < 1000000 {
			return fmt.Errorf("connection-retry-sec: cannot be smaller than 1s.")
		}

		return nil

	default:
		return fmt.Errorf("unsupported configuration option %q", key)
	}
}

func validateNTPServers(servers []any) error {
	for _, serverAny := range servers {
		server, ok := serverAny.(string)
		if !ok {
			return fmt.Errorf("%q is not a valid string", serverAny)
		}
		if err := validateServerName(server); err != nil {
			return err
		}
	}

	return nil
}

func validateServerName(serverAddress string) error {
	if validIPv4(serverAddress) == nil && !validHostname(serverAddress) {
		return fmt.Errorf("%q is not a valid server name", serverAddress)
	}
	return nil
}

func convertSystemdTimespanToUs(span string) (timeSpanUs int64, err error) {
	// The most reliable way to parse the timespans appears to be having
	// systemd-analyze do it
	// We also use this to compare min and max values as input validation
	cmd := exec.Command("systemd-analyze", "timespan", span)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid systemd.time timespan", span)
	}

	// Look for the line containing "μs:" or "us:" and capture the following digits
	re := regexp.MustCompile(`(?:μs|us):\s*(\d+)`)
	matches := re.FindStringSubmatch(string(output))

	// We did not capture the two parts of the line
	if len(matches) < 2 {
		return 0, fmt.Errorf("%q is not a valid systemd.time timespan", span)
	}

	// Parse the captured string digits into an int64
	us, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid systemd.time timespan", span)
	}

	return us, nil
}

// The "parse" function is used for converting the value to a microsecond value
// It implicitly checks its format, so we re-use it for validation
func validateSystemdTimeSpanFormat(span string) (timeSpanUs int64, err error) {
	return convertSystemdTimespanToUs(span)
}

func handleNTPConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	var cfg map[string]any
	if err := tr.Get("core", "system.ntp", &cfg); err != nil && !config.IsNoOption(err) {
		return fmt.Errorf("cannot get NTP config: %v", err)
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		// runtime system
		rootDir = opts.RootDir
	}

	// Check that the updated configuration is valid
	if err := validateNTPSettings(tr); err != nil {
		return err
	}

	// Create systemd configuration folder, if not present
	systemdConfigFolder := filepath.Join(rootDir, "/etc/systemd/")
	if err := os.MkdirAll(systemdConfigFolder, 0755); err != nil {
		return err
	}

	// Write validated configuration to file
	ntpConfigPath := filepath.Join(systemdConfigFolder, "/timesyncd.conf")
	if err := osutil.AtomicWriteFile(ntpConfigPath, serializeNTPConfiguration(cfg), 0644, 0); err != nil {
		return fmt.Errorf("cannot write NTP configuration: %v", err)
	}

	// Restart systemd-timesyncd.service to pick up the updated configuration
	sysd := systemd.New(systemd.SystemMode, &sysdLogger{})
	if err := sysd.ReloadOrRestart([]string{"systemd-timesyncd.service"}); err != nil {
		return fmt.Errorf("cannot restart timesyncd daemon after configuration change: %v", err)
	}

	return nil
}

func serializeNTPConfiguration(config map[string]any) (result []byte) {
	unitOptions := []*unit.UnitOption{}

	for k, v := range config {
		unitOptionKey := mapSnapOptionNameToUnitOptionName(k)
		unitOptionValue, _ := mapSnapOptionValueToUnitOptionValue(v)
		unitOption := *unit.NewUnitOption(
			"Time",
			unitOptionKey,
			unitOptionValue,
		)
		unitOptions = append(unitOptions, &unitOption)
	}

	byteStream, _ := io.ReadAll(unit.Serialize(unitOptions))
	return byteStream
}

func getNTPFromSystemHelper(key string) (result any, err error) {
	return getNTPFromSystem()
}

func mapUnitOptionValueToSnapOptionValue(option *unit.UnitOption) (result any) {
	switch option.Name {
	case "NTP", "FallbackNTP":
		return strings.Split(option.Value, " ")

	case "RootDistanceMaxSec", "PollIntervalMinSec", "PollIntervalMaxSec", "ConnectionRetrySec", "SaveIntervalSec":
		return option.Value

	default:
		return ""
	}
}

func mapSnapOptionValueToUnitOptionValue(option any) (string, error) {
	switch option := option.(type) {
	case []any:
		// Snapd will return a []any for lists. We need to check
		// that each element is a string manually
		var builder []string
		for _, server := range option {
			if s, ok := server.(string); ok {
				builder = append(builder, s)
			}
		}

		return strings.Join(builder, " "), nil

	case string:
		return option, nil

	case json.Number:
		return option.String() + "s", nil

	default:
		return "", fmt.Errorf("invalid option type: %T", option)
	}
}

func mapUnitOptionNametoSnapOptionName(timesyncdOption string) string {
	return timesyncdToSnapKeyMapping[timesyncdOption]
}

func mapSnapOptionNameToUnitOptionName(snapOption string) string {
	return snapToTimesyncdKeyMapping[snapOption]
}

func getNTPFromSystem() (result map[string]any, err error) {
	if release.OnClassic {
		return nil, nil
	}

	file, err := os.Open("/etc/systemd/timesyncd.conf")
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read NTP configuration file /etc/systemd/timesyncd.conf: %v", err)
	}
	defer file.Close()

	unitOptions, err := unit.Deserialize(file)
	if err != nil {
		return nil, fmt.Errorf("cannot parse systemd unit in configuration file /etc/systemd/timesyncd.conf: %v", err)
	}

	val := map[string]any{}

	for _, option := range unitOptions {
		snapOptionName := mapUnitOptionNametoSnapOptionName(option.Name)
		snapOptionValue := mapUnitOptionValueToSnapOptionValue(option)
		val[snapOptionName] = snapOptionValue
	}

	return val, nil
}
