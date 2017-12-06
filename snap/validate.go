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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/spdx"
	"github.com/snapcore/snapd/strutil"
)

// Regular expressions describing correct identifiers.
//
// validSnapName is also used to validate socket identifiers.
var validSnapName = regexp.MustCompile("^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$")
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

// validateSocketName checks if a string ca be used as a name for a socket (for
// socket activation).
func validateSocketName(name string) error {
	valid := validSnapName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid socket name: %q", name)
	}
	return nil
}

// validateSocketmode checks that the socket mode is a valid file mode.
func validateSocketMode(mode os.FileMode) error {
	if mode > 0777 {
		return fmt.Errorf("cannot use socket mode: %04o", mode)
	}

	return nil
}

// validateSocketAddr checks that the value of socket addresses.
func validateSocketAddr(socket *SocketInfo, fieldName string, address string) error {
	if address == "" {
		return fmt.Errorf("socket %q must define %q", socket.Name, fieldName)
	}

	switch address[0] {
	case '/', '$':
		return validateSocketAddrPath(socket, fieldName, address)
	case '@':
		return validateSocketAddrAbstract(socket, fieldName, address)
	default:
		return validateSocketAddrNet(socket, fieldName, address)
	}
}

func validateSocketAddrPath(socket *SocketInfo, fieldName string, path string) error {
	if clean := filepath.Clean(path); clean != path {
		return fmt.Errorf("socket %q has invalid %q: %q should be written as %q", socket.Name, fieldName, path, clean)
	}

	if !(strings.HasPrefix(path, "$SNAP_DATA/") || strings.HasPrefix(path, "$SNAP_COMMON/")) {
		return fmt.Errorf(
			"socket %q has invalid %q: only $SNAP_DATA and $SNAP_COMMON prefixes are allowed", socket.Name, fieldName)
	}

	return nil
}

func validateSocketAddrAbstract(socket *SocketInfo, fieldName string, path string) error {
	prefix := fmt.Sprintf("@snap.%s.", socket.App.Snap.Name())
	if !strings.HasPrefix(path, prefix) {
		return fmt.Errorf("socket %q path for %q must be prefixed with %q", socket.Name, fieldName, prefix)
	}
	return nil
}

func validateSocketAddrNet(socket *SocketInfo, fieldName string, address string) error {
	lastIndex := strings.LastIndex(address, ":")
	if lastIndex >= 0 {
		if err := validateSocketAddrNetHost(socket, fieldName, address[:lastIndex]); err != nil {
			return err
		}
		if err := validateSocketAddrNetPort(socket, fieldName, address[lastIndex+1:]); err != nil {
			return err
		}
		return nil
	}

	// Address only contains a port
	if err := validateSocketAddrNetPort(socket, fieldName, address); err != nil {
		return err
	}

	return nil
}

func validateSocketAddrNetHost(socket *SocketInfo, fieldName string, address string) error {
	for _, validAddress := range []string{"127.0.0.1", "[::1]", "[::]"} {
		if address == validAddress {
			return nil
		}
	}

	return fmt.Errorf("socket %q has invalid %q address %q", socket.Name, fieldName, address)
}

func validateSocketAddrNetPort(socket *SocketInfo, fieldName string, port string) error {
	var val uint64
	var err error
	retErr := fmt.Errorf("socket %q has invalid %q port number %q", socket.Name, fieldName, port)
	if val, err = strconv.ParseUint(port, 10, 16); err != nil {
		return retErr
	}
	if val < 1 || val > 65535 {
		return retErr
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

	err = info.Epoch.Validate()
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

	// validate apps ordering according to after/before
	if err := validateAppsOrdering(info); err != nil {
		return err
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

func validateAppSocket(socket *SocketInfo) error {
	if err := validateSocketName(socket.Name); err != nil {
		return err
	}

	if err := validateSocketMode(socket.SocketMode); err != nil {
		return err
	}
	return validateSocketAddr(socket, "listen-stream", socket.ListenStream)
}

type appStartupOrder int

const (
	orderBefore appStartupOrder = iota
	orderAfter
)

func (a appStartupOrder) String() string {
	switch a {
	case orderBefore:
		return "before"
	case orderAfter:
		return "after"
	}
	return fmt.Sprintf("<unsupported order: %v>", int(a))
}

func appStartupOrdering(app *AppInfo, order appStartupOrder) []string {
	switch order {
	case orderBefore:
		return app.Before
	case orderAfter:
		return app.After
	}
	panic(fmt.Sprintf("unsupported order: %v", order))
}

func isAppOrdered(app *AppInfo, order appStartupOrder, other *AppInfo) bool {
	deps := appStartupOrdering(app, order)
	if len(deps) > 0 && strutil.ListContains(deps, other.Name) {
		return true
	}
	return false
}

// maybeOrderBeforeAfter does naiive ordering of apps using Before/After
// criteria and modifies the provided apps list. The caller must verify if all
// dependencies are satisfied.
func maybeOrderBeforeAfter(apps []*AppInfo) {

	// perform simple sorting of apps, apps that start before current must
	// be in the left side of the list, apps that start after current must
	// be in the right side of the list

	for i := range apps {
		for j := range apps {
			if j == i {
				continue
			}
			var swap bool
			if !swap && i < j && isAppOrdered(apps[i], orderAfter, apps[j]) {
				// app i in lhs but ordered after j
				swap = true
			}
			if !swap && i > j && isAppOrdered(apps[i], orderBefore, apps[j]) {
				// app i in rhs but ordered before j
				swap = true
			}
			if !swap && j < i && isAppOrdered(apps[j], orderAfter, apps[i]) {
				// app j in lhs but ordered after i
				swap = true
			}
			if !swap && j > i && isAppOrdered(apps[j], orderBefore, apps[i]) {
				// app j in rhs but ordered before i
				swap = true
			}
			if swap {
				tmp := apps[i]
				apps[i] = apps[j]
				apps[j] = tmp
			}
		}
	}
}

func validateAppsOrdering(snap *Info) error {
	apps := make([]*AppInfo, 0, len(snap.Apps))
	for _, app := range snap.Apps {
		apps = append(apps, app)
	}

	maybeOrderBeforeAfter(apps)

	appsListContains := func(apps []*AppInfo, names []string) bool {
		for _, name := range names {
			var found bool
			for _, app := range apps {
				if app.Name == name {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	// verify if app dependencies are satisfied
	for i := range apps {
		after := appsListContains(apps[0:i], apps[i].After)
		before := appsListContains(apps[i+1:], apps[i].Before)
		if !after || !before {
			whichFailed := "after"
			if !after && !before {
				whichFailed = "after,before"
			} else if !before {
				whichFailed = "before"
			}
			return fmt.Errorf("cannot validate app %q startup ordering: %q dependencies cannot be satisfied",
				apps[i].Name, whichFailed)
		}
	}
	return nil
}

func validateAppStartupOrdering(app *AppInfo, order appStartupOrder) error {
	ordering := appStartupOrdering(app, order)

	// we must be a service to request ordering
	if len(ordering) > 0 && !app.IsService() {
		return fmt.Errorf("cannot validate app %q: requires startup ordering, but is not a service",
			app.Name)
	}

	for _, dep := range ordering {
		// dependency is not defined
		other, ok := app.Snap.Apps[dep]
		if !ok {
			return fmt.Errorf("cannot validate app %q startup ordering: needs to be started %s %q, but %q is not defined",
				app.Name, order, dep, dep)
		}

		if !other.IsService() {
			return fmt.Errorf("cannot validate app %q startup ordering: dependent app %q is not a service",
				app.Name, dep)
		}
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

	// Socket activation requires the "network-bind" plug
	if len(app.Sockets) > 0 {
		if _, ok := app.Plugs["network-bind"]; !ok {
			return fmt.Errorf(`"network-bind" interface plug is required when sockets are used`)
		}
	}

	for _, socket := range app.Sockets {
		err := validateAppSocket(socket)
		if err != nil {
			return err
		}
	}

	if err := validateAppStartupOrdering(app, orderBefore); err != nil {
		return err
	}
	if err := validateAppStartupOrdering(app, orderAfter); err != nil {
		return err
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
