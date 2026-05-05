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
	"path/filepath"
	"regexp"
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
	// and register it as a external config
	config.RegisterExternalConfig("core", "system.ntp", getNTPFromSystemHelper)
}

// Too simplistic?
var validHostname = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9]))*$`).MatchString
var validIPv4 = net.ParseIP

var timesyncdToSnapKeyMapping = map[string]string{
	"NTP":         "servers",
	"FallbackNTP": "fallback-servers",
}

var snapToTimesyncdKeyMapping = map[string]string{
	"servers":          "NTP",
	"fallback-servers": "FallbackNTP",
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
		unitOption := *unit.NewUnitOption(
			"Time",
			mapKeySnapToTimesyncd(k),
			mapValueSnapToTimesyncd(v),
		)
		unitOptions = append(unitOptions, &unitOption)
	}

	byteStream, _ := io.ReadAll(unit.Serialize(unitOptions))
	return byteStream
}

func getNTPFromSystemHelper(key string) (result any, err error) {
	return getNTPFromSystem()
}

func mapValueTimesyncdToSnap(option *unit.UnitOption) (result any) {
	switch option.Name {
	case "NTP", "FallbackNTP":
		return strings.Split(option.Value, " ")

	default:
		return ""
	}
}

func mapValueSnapToTimesyncd(option any) string {
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

		return strings.Join(builder, " ")

	default:
		return ""
	}
}

func mapKeyTimesyncdToSnap(timesyncdOption string) string {
	return timesyncdToSnapKeyMapping[timesyncdOption]
}

func mapKeySnapToTimesyncd(snapOption string) string {
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
		snapOptionName := mapKeyTimesyncdToSnap(option.Name)
		snapOptionValue := mapValueTimesyncdToSnap(option)
		val[snapOptionName] = snapOptionValue
	}

	return val, nil
}
