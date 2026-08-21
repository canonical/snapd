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
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	supportedConfigurations["core.system.ntp"] = true
	supportedConfigurations["core.system.ntp.servers"] = true
	supportedConfigurations["core.system.ntp.fallback-servers"] = true
	supportedConfigurations["core.system.ntp.max-root-time-distance"] = true
	supportedConfigurations["core.system.ntp.min-poll-interval"] = true
	supportedConfigurations["core.system.ntp.max-poll-interval"] = true
	supportedConfigurations["core.system.ntp.connection-retry-interval"] = true
	supportedConfigurations["core.system.ntp.save-interval"] = true
	// and register it as a external config
	config.RegisterExternalConfig("core", "system.ntp", getNTPFromSystemHelper)
}

// Too simplistic?
var validHostname = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9]))*$`).MatchString

// Match the line containing "μs:" or "us:" and capture the following digits
var timespanUsRegexp = regexp.MustCompile(`(?:μs|us):\s*(\d+)`)

var timesyncdToSnapKeyMapping = map[string]string{
	"NTP":                "servers",
	"FallbackNTP":        "fallback-servers",
	"RootDistanceMaxSec": "max-root-time-distance",
	"PollIntervalMinSec": "min-poll-interval",
	"PollIntervalMaxSec": "max-poll-interval",
	"ConnectionRetrySec": "connection-retry-interval",
	"SaveIntervalSec":    "save-interval",
}

var snapToTimesyncdKeyMapping = map[string]string{
	"servers":                   "NTP",
	"fallback-servers":          "FallbackNTP",
	"max-root-time-distance":    "RootDistanceMaxSec",
	"min-poll-interval":         "PollIntervalMinSec",
	"max-poll-interval":         "PollIntervalMaxSec",
	"connection-retry-interval": "ConnectionRetrySec",
	"save-interval":             "SaveIntervalSec",
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

	// Validate that min-poll-interval < max-poll-interval.
	// Use the systemd-timesyncd defaults if they have not been overwritten by the user.
	// The "*String" variables are used for error messages to show the user the exact
	// input they provided in case of an error, instead of a serialized Duration, which might be different
	// (e.g. printing "1m0s" when the user input "1m").
	ntpCfgTimeWithDefault := func(cfgName, defString string) (time.Duration, string) {
		valString := defString
		if v, exists := ntpCfg[cfgName]; exists {
			valString = v.(string)
		}
		retVal, _ := time.ParseDuration(valString)
		return retVal, valString
	}

	pollIntervalMin, pollIntervalMinString := ntpCfgTimeWithDefault("min-poll-interval", "32s")
	pollIntervalMax, pollIntervalMaxString := ntpCfgTimeWithDefault("max-poll-interval", "2048s")

	if pollIntervalMin > pollIntervalMax {
		return fmt.Errorf("invalid NTP configuration: min-poll-interval (%q) cannot be greater than max-poll-interval (%q)", pollIntervalMinString, pollIntervalMaxString)
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
		return validateNTPServers(servers)

	case "max-root-time-distance", "min-poll-interval", "max-poll-interval", "connection-retry-interval", "save-interval":
		valueStr, ok := value.(string)
		if !ok {
			return fmt.Errorf("%v is not a string", key)
		}
		if strings.TrimSpace(valueStr) != valueStr {
			return fmt.Errorf("%v: contains leading or trailing whitespace", key)
		}

		// The value that the user inputs should be parsed as a Go duration string for consistency
		// with other configuration options
		duration, err := time.ParseDuration(valueStr)
		if err != nil {
			return fmt.Errorf("%v: %v", key, err)
		}
		if duration < 0 {
			return fmt.Errorf("%v: duration %q cannot be negative", key, valueStr)
		}
		// systemd's minimum resolution is 1µs; nanosecond values would be silently
		// rounded or rejected by timesyncd.
		if duration != 0 && duration < time.Microsecond {
			return fmt.Errorf("%v: duration %q is below systemd's minimum resolution of 1µs", key, valueStr)
		}
		if key == "min-poll-interval" && duration < 16*time.Second {
			return fmt.Errorf("min-poll-interval: cannot be smaller than 16s")
		}
		if key == "connection-retry-interval" && duration < 1*time.Second {
			return fmt.Errorf("connection-retry-interval: cannot be smaller than 1s")
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
			return fmt.Errorf("server %v is not a valid string", serverAny)
		}
		if err := validateServerName(server); err != nil {
			return err
		}
	}
	return nil
}

func validateServerName(serverAddress string) error {
	if net.ParseIP(serverAddress) == nil && !validHostname(serverAddress) {
		return fmt.Errorf("%q is not a valid server name", serverAddress)
	}
	return nil
}

func convertSystemdTimespanToUs(span string) (timeSpanUs int64, err error) {
	// The most reliable way to parse the timespans appears to be having
	// systemd-analyze do it
	cmd := exec.Command("systemd-analyze", "timespan", span)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid systemd.time timespan", span)
	}

	// Look for the line containing "μs:" or "us:" and capture the following digits
	matches := timespanUsRegexp.FindStringSubmatch(string(output))
	// We did not capture the two parts of the line ("us:" and the digits)
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

func convertSystemdTimespanToGoDurationString(span string) (string, error) {
	us, err := convertSystemdTimespanToUs(span)
	if err != nil {
		return "", err
	}

	const maxDurationUs = int64(time.Duration(1<<63-1) / time.Microsecond)
	if us > maxDurationUs {
		return "", fmt.Errorf("%q is too large for a Go duration", span)
	}
	return (time.Duration(us) * time.Microsecond).String(), nil
}

// ntpConfigurationDeepEqual compares two NTP configurations and returns true if they are equal,
// false otherwise.
// The standard reflect.DeepEqual cannot be used to compare the configurations as the values
// of fields "servers" and "fallback-servers" parsed by unit.Deserialize (i.e. the oldConfig)
// are of type []string, while the ones coming from snapd are of type []any.
// reflect.DeepEqual considers them as different, but we want to consider them as equal as
// long as their content is the same.
func ntpConfigurationDeepEqual(oldConfig, newConfig map[string]any) bool {
	// Check if both maps have the same number of keys
	// Automatically handles empty and nil maps
	if len(oldConfig) != len(newConfig) {
		return false
	}

	for key, oldVal := range oldConfig {
		newVal, exists := newConfig[key]
		if !exists {
			return false
		}

		// Use the mapping function to convert all values to their string representation and
		// compare those
		// Takes case of converting and comparing []any (config read from snapd) and []string
		// (config read from the config file)
		oldValString, err := mapOptionValueSnapToTimesyncd(oldVal)
		if err != nil {
			return false
		}
		newValString, err := mapOptionValueSnapToTimesyncd(newVal)
		if err != nil {
			return false
		}

		// For duration fields, compare as time.Duration values so that equivalent
		// representations like "40m" (user input) and "40m0s" (normalised from disk)
		// are treated as equal. Server list fields cannot be parsed as durations so
		// they fall back to plain string comparison.
		oldDur, oldErr := time.ParseDuration(oldValString)
		newDur, newErr := time.ParseDuration(newValString)
		if oldErr == nil && newErr == nil {
			if oldDur != newDur {
				return false
			}
		} else if oldValString != newValString {
			return false
		}
	}
	return true
}

func handleNTPConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	var cfg map[string]any
	err := tr.Get("core", "system.ntp", &cfg)
	if config.IsNoOption(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot get NTP config: %v", err)
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		// filesystem-only context (e.g. image build)
		rootDir = opts.RootDir
	} else {
		oldConfig, err := getNTPFromSystem()
		if err != nil {
			return err
		}
		if ntpConfigurationDeepEqual(oldConfig, cfg) {
			// If the configuration has not changed, do nothing.
			return nil
		}
	}

	// Create systemd configuration folder, if not present
	systemdConfigFolder := filepath.Join(rootDir, "etc", "systemd")
	if err := os.MkdirAll(systemdConfigFolder, 0755); err != nil {
		return err
	}

	// Configuration file path
	// We write the main configuration file directly and not use drop-ins
	// to make the reading operation simpler, and to avoid discrepancies
	// between `snap get` and `snap set` when other drop-in files are installed
	// on the system, though the result might be be imprecise compared to which
	// configuration values are read by timesyncd. More explanation is found in the
	// docstring for getNTPFromSystem.
	ntpConfigPath := filepath.Join(systemdConfigFolder, "timesyncd.conf")

	if len(cfg) == 0 {
		// If the config is empty, we want to reset to defaults, which is achieved by deleting the configuration file
		if err := os.Remove(ntpConfigPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot reset NTP configuration to defaults: %v", err)
		}
	} else {
		// Otherwise, we overwrite the file with the new configuration
		if err := osutil.AtomicWriteFile(ntpConfigPath, serializeNTPConfiguration(cfg), 0644, 0); err != nil {
			return fmt.Errorf("cannot write NTP configuration: %v", err)
		}
	}

	// Restart systemd-timesyncd.service to pick up the updated configuration (runtime only).
	if opts == nil {
		sysd := systemd.New(systemd.SystemMode, &sysdLogger{})
		if err := sysd.ReloadOrRestart([]string{"systemd-timesyncd.service"}); err != nil {
			return fmt.Errorf("cannot restart timesyncd daemon after configuration change: %v", err)
		}
	}
	return nil
}

func serializeNTPConfiguration(config map[string]any) (result []byte) {
	unitOptions := []*unit.UnitOption{}
	for k, v := range config {
		unitOptionKey := mapOptionNameSnapToTimesyncd(k)
		unitOptionValue, _ := mapOptionValueSnapToTimesyncd(v)
		unitOption := *unit.NewUnitOption(
			"Time",
			unitOptionKey,
			unitOptionValue,
		)
		unitOptions = append(unitOptions, &unitOption)
	}

	// unit.Serialize should not meet I/O errors as it's reading from memory
	byteStream, _ := io.ReadAll(unit.Serialize(unitOptions))
	return byteStream
}

// getNTPFromSystemHelper is the callback registered for the "system.ntp" key.
// The key argument is ignored because getNTPFromSystem returns the full configuration
// map and the relevant subkeys are selected by other components before being shown
// to the user
func getNTPFromSystemHelper(key string) (result any, err error) {
	return getNTPFromSystem()
}

func mapOptionValueTimesyncdToSnap(option *unit.UnitOption) (result any, err error) {
	switch option.Name {
	case "NTP", "FallbackNTP":
		trimmedString := strings.TrimSpace(option.Value)
		if len(trimmedString) == 0 {
			return []string{}, nil
		}
		return strings.Fields(trimmedString), nil

	case "RootDistanceMaxSec", "PollIntervalMinSec", "PollIntervalMaxSec", "ConnectionRetrySec", "SaveIntervalSec":
		return convertSystemdTimespanToGoDurationString(strings.TrimSpace(option.Value))

	default:
		return "", nil
	}
}

func mapOptionValueSnapToTimesyncd(option any) (string, error) {
	switch option := option.(type) {
	case []string:
		// Matches fields "servers" and "fallback-servers" parsed from the current
		// configuration file
		return strings.Join(option, " "), nil

	case []any:
		// Snapd will return a []any for lists. We need to check
		// that each element is a string manually
		var builder []string
		for _, server := range option {
			s, ok := server.(string)
			if !ok {
				return "", fmt.Errorf("list contains non-string element: %T", server)
			}
			builder = append(builder, s)
		}
		return strings.Join(builder, " "), nil

	case string:
		// Matches timespan fields that are set as a Go duration string (e.g. "min-poll-interval": "32s").
		return strings.ReplaceAll(option, "µs", "us"), nil

	default:
		return "", fmt.Errorf("invalid option type: %T", option)
	}
}

func mapOptionNameTimesyncdToSnap(timesyncdOption string) string {
	if v, ok := timesyncdToSnapKeyMapping[timesyncdOption]; ok {
		return v
	}
	return ""
}

func mapOptionNameSnapToTimesyncd(snapOption string) string {
	if v, ok := snapToTimesyncdKeyMapping[snapOption]; ok {
		return v
	}
	return ""
}

// getNTPFromSystem reads /etc/systemd/timesyncd.conf and returns its contents as a map.
// Keys are translated from timesyncd names (e.g. "NTP", "RootDistanceMaxSec") to snapd-style
// names (e.g. "servers", "max-root-time-distance"). Space-separated server lists become string
// slices, and systemd.time duration values are converted to Go duration strings.
// Returns nil (no error) on classic systems, when the file is absent (default timesyncd
// settings apply), or when no recognised options are present.
// The configuration is read from and written to the main file ignoring drop-ins that might be
// present on the system. This should be rare on Ubuntu Core. This is a deliberate choice, that
// simplifies the reading process and avoids merging of the drop-ins in the code.
// The limitation is that the output of this function is not correct if drop-ins are installed
// inside /etc/systemd/timesyncd.conf.d/.
// Compared to the return value of getNTPFromSystem, the values set to timesyncd will have
// the server lists in the drop-ins appended to the one from the main file, and the duration values
// overwritten by the ones in the drop-ins.
// This cannot be avoided by writing the snapd configuration to a drop-in file, as it is always
// possible for the users and applications to install a configuration file that comes later in
// alphabetical order.
func getNTPFromSystem() (result map[string]any, err error) {
	if release.OnClassic {
		return nil, nil
	}

	file, err := os.Open(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "timesyncd.conf"))
	if os.IsNotExist(err) {
		// A missing file is not an error, it just means that there is no custom configuration and
		//  the system is using the defaults
		return nil, nil
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
		snapOptionName := mapOptionNameTimesyncdToSnap(option.Name)
		if snapOptionName == "" {
			// If the option name is empty, it means the option is not supported, so we skip it
			continue
		}
		snapOptionValue, err := mapOptionValueTimesyncdToSnap(option)
		if err != nil {
			return nil, fmt.Errorf("cannot parse NTP option: %s: %v", snapOptionName, err)
		}
		val[snapOptionName] = snapOptionValue
	}

	// Do not return an empty config "system.ntp {}" if there is no custom configuration in the file
	// We check here instead of checking the length of unitOptions, since we skip unsupported options
	if len(val) == 0 {
		return nil, nil
	}
	return val, nil
}
