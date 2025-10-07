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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

const (
	securityJournalSocketUnit     = "systemd-journald@snapd-security.socket"
	securityJournalServiceUnit    = "systemd-journald@snapd-security.service"
	securityJournalConfPath       = "/etc/systemd/journald@snapd-security.conf"
	securityJournalDefaultMaxSize = "10M"
)

var (
	seclogEnable  = seclog.Enable
	seclogDisable = seclog.Disable
)

func init() {
	supportedConfigurations["core.security-logging.enabled"] = true
	supportedConfigurations["core.security-logging.max-size"] = true
}

func validateSecurityLoggingSettings(tr ConfGetter) error {
	if err := validateBoolFlag(tr, "security-logging.enabled"); err != nil {
		return err
	}

	maxSize, err := coreCfg(tr, "security-logging.max-size")
	if err != nil {
		return err
	}
	if maxSize != "" {
		if err := validateJournalSizeValue(maxSize); err != nil {
			return fmt.Errorf("security-logging.max-size %v", err)
		}
	}

	return nil
}

func handleSecurityLoggingConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	enabled, err := coreCfg(tr, "security-logging.enabled")
	if err != nil {
		return err
	}
	maxSize, err := coreCfg(tr, "security-logging.max-size")
	if err != nil {
		return err
	}

	// If nothing is set, do nothing.
	if enabled == "" && maxSize == "" {
		return nil
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	switch enabled {
	case "false":
		return disableSecurityLogging(rootDir, opts)
	default:
		return enableSecurityLogging(rootDir, opts, maxSize)
	}
}

func disableSecurityLogging(rootDir string, opts *fsOnlyContext) error {
	// Disconnect from the journal namespace first so that the sink
	// is closed before we tear down the service.
	if err := seclogDisable(); err != nil {
		logger.Noticef("cannot disable security logger: %v", err)
	}

	if opts != nil {
		// During filesystem-only apply, mask the socket by creating
		// a symlink to /dev/null, mirroring what systemctl mask does.
		maskDir := filepath.Join(rootDir, "/etc/systemd/system")
		if err := os.MkdirAll(maskDir, 0755); err != nil {
			return err
		}
		maskPath := filepath.Join(maskDir, securityJournalSocketUnit)
		os.Remove(maskPath)
		return os.Symlink("/dev/null", maskPath)
	}

	sysd := systemd.NewUnderRoot(rootDir, systemd.SystemMode, nil)
	// Mask the socket to prevent future activation. The running
	// journald instance (if any) will exit on its own once idle.
	if err := sysd.Mask(securityJournalSocketUnit); err != nil {
		return err
	}
	return nil
}

func enableSecurityLogging(rootDir string, opts *fsOnlyContext, maxSize string) error {
	confPath := filepath.Join(rootDir, securityJournalConfPath)

	// Write the namespace config file with current settings.
	conf := generateSecurityJournalConf(maxSize)
	if err := osutil.AtomicWriteFile(confPath, conf, 0644, 0); err != nil {
		return err
	}

	if opts != nil {
		// Filesystem-only apply; remove any mask symlink that may
		// have been created by a previous disable.
		maskPath := filepath.Join(rootDir, "/etc/systemd/system", securityJournalSocketUnit)
		os.Remove(maskPath)
		return nil
	}

	sysd := systemd.NewUnderRoot(rootDir, systemd.SystemMode, nil)

	// Unmask in case it was previously disabled.
	if err := sysd.Unmask(securityJournalSocketUnit); err != nil {
		return err
	}

	// Start the socket unit so socket activation is available immediately.
	if err := sysd.Start([]string{securityJournalSocketUnit}); err != nil {
		logger.Noticef("cannot start security journal socket: %v", err)
	}

	// Signal the namespaced journald to reload configuration if it was
	// already running; if not, socket activation picks up the new config.
	if err := sysd.Kill(securityJournalServiceUnit, "USR1", ""); err != nil {
		// Non-fatal: new config takes effect on next activation.
	}

	// Re-open the security logger against the fresh namespace connection.
	// This is done last so that the journal service is in a stable state
	// before we connect to the sink.
	if err := seclogEnable(); err != nil {
		logger.Noticef("cannot enable security logger: %v", err)
	}

	return nil
}

func generateSecurityJournalConf(maxSize string) []byte {
	conf := "[Journal]\nStorage=persistent\nCompress=yes\n"
	if maxSize == "" {
		maxSize = securityJournalDefaultMaxSize
	}
	conf += fmt.Sprintf("SystemMaxUse=%s\n", maxSize)
	conf += "SyncIntervalSec=30s\nSyncOnShutdown=yes\n"
	// Sanity rate limit: generous enough to never trigger under
	// normal operation, but prevents runaway log storms from
	// filling the journal if something goes very wrong.
	conf += "RateLimitIntervalSec=30s\nRateLimitBurst=10000\n"
	return []byte(conf)
}

// validateJournalSizeValue validates a systemd journal size value (e.g. "10M", "1G").
// The minimum allowed value is 10M.
func validateJournalSizeValue(value string) error {
	if len(value) < 2 {
		return fmt.Errorf("cannot parse size %q: must be a number followed by a suffix like K, M, G or T", value)
	}
	suffix := value[len(value)-1]
	var multiplier uint64
	switch suffix {
	case 'K':
		multiplier = 1024
	case 'M':
		multiplier = 1024 * 1024
	case 'G':
		multiplier = 1024 * 1024 * 1024
	case 'T':
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return fmt.Errorf("cannot parse size %q: must be a number followed by a suffix like K, M, G or T", value)
	}
	numStr := value[:len(value)-1]
	var num uint64
	for _, ch := range numStr {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("cannot parse size %q: must be a number followed by a suffix like K, M, G or T", value)
		}
		num = num*10 + uint64(ch-'0')
	}
	const minSize = 10 * 1024 * 1024 // 10M
	if num*multiplier < minSize {
		return fmt.Errorf("cannot set size %q: must be at least 10M", value)
	}
	return nil
}
