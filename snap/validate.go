// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/spdx"
)

// Regular expression describing correct identifiers.
var validSnapName = regexp.MustCompile("^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$")
var validEpoch = regexp.MustCompile("^(?:0|[1-9][0-9]*[*]?)$")
var validHookName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a snap name.
func ValidateName(name string) error {
	// NOTE: This function should be synchronized with the two other
	// implementations: sc_snap_name_validate and validate_snap_name .
	valid := validSnapName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid snap name: %q", name)
	}
	return nil
}

// ValidateEpoch checks if a string can be used as a snap epoch.
func ValidateEpoch(epoch string) error {
	valid := validEpoch.MatchString(epoch)
	if !valid {
		return fmt.Errorf("invalid snap epoch: %q", epoch)
	}
	return nil
}

// ValidateLicense checks if a string is a valid SPDX expression.
func ValidateLicense(license string) error {
	if err := spdx.ValidateLicense(license); err != nil {
		return fmt.Errorf("cannot validate license %q: %s", license, err)
	}
	return nil
}

// ValidateHook validates the content of the given HookInfo
func ValidateHook(hook *HookInfo) error {
	valid := validHookName.MatchString(hook.Name)
	if !valid {
		return fmt.Errorf("invalid hook name: %q", hook.Name)
	}
	return nil
}

var validAlias = regexp.MustCompile("^[a-zA-Z0-9][-_.a-zA-Z0-9]*$")

// ValidateAlias checks if a string can be used as an alias name.
func ValidateAlias(alias string) error {
	valid := validAlias.MatchString(alias)
	if !valid {
		return fmt.Errorf("invalid alias name: %q", alias)
	}
	return nil
}

// Validate verifies the content in the info.
func Validate(info *Info) error {
	name := info.Name()
	if name == "" {
		return fmt.Errorf("snap name cannot be empty")
	}
	err := ValidateName(name)
	if err != nil {
		return err
	}

	epoch := info.Epoch
	if epoch == "" {
		return fmt.Errorf("snap epoch cannot be empty")
	}
	err = ValidateEpoch(epoch)
	if err != nil {
		return err
	}

	license := info.License
	if license != "" {
		err := ValidateLicense(license)
		if err != nil {
			return err
		}
	}

	// validate app entries
	for _, app := range info.Apps {
		err := ValidateApp(app)
		if err != nil {
			return err
		}
	}

	// validate aliases
	for alias, app := range info.LegacyAliases {
		if !validAlias.MatchString(alias) {
			return fmt.Errorf("cannot have %q as alias name for app %q - use only letters, digits, dash, underscore and dot characters", alias, app.Name)
		}
	}

	// validate hook entries
	for _, hook := range info.Hooks {
		err := ValidateHook(hook)
		if err != nil {
			return err
		}
	}

	// ensure that plug and slot have unique names
	if err := plugsSlotsUniqueNames(info); err != nil {
		return err
	}

	for _, layout := range info.Layout {
		if err := ValidateLayout(layout); err != nil {
			return err
		}
	}
	return nil
}

func plugsSlotsUniqueNames(info *Info) error {
	// we could choose the smaller collection if we wanted to optimize this check
	for plugName := range info.Plugs {
		if info.Slots[plugName] != nil {
			return fmt.Errorf("cannot have plug and slot with the same name: %q", plugName)
		}
	}
	return nil
}

func validateField(name, cont string, whitelist *regexp.Regexp) error {
	if !whitelist.MatchString(cont) {
		return fmt.Errorf("app description field '%s' contains illegal %q (legal: '%s')", name, cont, whitelist)

	}
	return nil
}

// appContentWhitelist is the whitelist of legal chars in the "apps"
// section of snap.yaml. Do not allow any of [',",`] here or snap-exec
// will get confused.
var appContentWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:$-]*$`)
var validAppName = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

// ValidateApp verifies the content in the app info.
func ValidateApp(app *AppInfo) error {
	switch app.Daemon {
	case "", "simple", "forking", "oneshot", "dbus", "notify":
		// valid
	default:
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	// Validate app name
	if !validAppName.MatchString(app.Name) {
		return fmt.Errorf("cannot have %q as app name - use letters, digits, and dash as separator", app.Name)
	}

	// Validate the rest of the app info
	checks := map[string]string{
		"command":           app.Command,
		"stop-command":      app.StopCommand,
		"reload-command":    app.ReloadCommand,
		"post-stop-command": app.PostStopCommand,
		"bus-name":          app.BusName,
	}

	for name, value := range checks {
		if err := validateField(name, value, appContentWhitelist); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePathVariables ensures that given path contains only $SNAP, $SNAP_DATA or $SNAP_COMMON.
func ValidatePathVariables(path string) error {
	for path != "" {
		start := strings.IndexRune(path, '$')
		if start < 0 {
			break
		}
		path = path[start+1:]
		end := strings.IndexFunc(path, func(c rune) bool {
			return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '_'
		})
		if end < 0 {
			end = len(path)
		}
		v := path[:end]
		if v != "SNAP" && v != "SNAP_DATA" && v != "SNAP_COMMON" {
			return fmt.Errorf("reference to unknown variable %q", "$"+v)
		}
		path = path[end:]
	}
	return nil
}

// ValidateLayout ensures that the given layout contains only valid subset of constructs.
func ValidateLayout(li *Layout) error {
	// The path is used to identify the layout below so validate it first.
	if li.Path == "" {
		return fmt.Errorf("cannot accept layout with empty path")
	} else {
		if err := ValidatePathVariables(li.Path); err != nil {
			return fmt.Errorf("cannot accept layout of %q: %s", li.Path, err)
		}
	}
	// Presence of the Bind, Type and Symlink fields implies kind of layout.
	if li.Bind == "" && li.Type == "" && li.Symlink == "" {
		return fmt.Errorf("cannot determine layout for %q", li.Path)
	}
	if (li.Bind != "" && li.Type != "") ||
		(li.Bind != "" && li.Symlink != "") ||
		(li.Type != "" && li.Symlink != "") {
		return fmt.Errorf("cannot accept conflicting layout for %q", li.Path)
	}
	if li.Bind != "" {
		if err := ValidatePathVariables(li.Bind); err != nil {
			return fmt.Errorf("cannot accept layout of %q: %s", li.Path, err)
		}
	}
	// Only the "tmpfs" filesystem is allowed.
	if li.Type != "" && li.Type != "tmpfs" {
		return fmt.Errorf("cannot accept filesystem %q for %q", li.Type, li.Path)
	}
	if li.Symlink != "" {
		if err := ValidatePathVariables(li.Symlink); err != nil {
			return fmt.Errorf("cannot accept layout of %q: %s", li.Path, err)
		}
	}
	// Only certain users and groups are allowed.
	// TODO: allow declared snap user and group names.
	if li.User != "" && li.User != "root" && li.User != "nobody" {
		return fmt.Errorf("cannot accept user %q for %q", li.User, li.Path)
	}
	if li.Group != "" && li.Group != "root" && li.Group != "nobody" {
		return fmt.Errorf("cannot accept group %q for %q", li.Group, li.Path)
	}
	// "at most" 0777 permissions are allowed.
	if li.Mode&^os.FileMode(0777) != 0 {
		return fmt.Errorf("cannot accept mode %#0o for %q", li.Mode, li.Path)
	}
	return nil
}
