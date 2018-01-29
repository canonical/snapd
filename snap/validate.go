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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/spdx"
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
	if err := validateAppOrderCycles(info.Apps); err != nil {
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

// validateAppOrderCycles checks for cycles in app ordering dependencies
func validateAppOrderCycles(apps map[string]*AppInfo) error {
	// list of successors of given app
	successors := make(map[string][]string, len(apps))
	// count of predecessors (i.e. incoming edges) of given app
	predecessors := make(map[string]int, len(apps))

	for _, app := range apps {
		for _, other := range app.After {
			predecessors[app.Name]++
			successors[other] = append(successors[other], app.Name)
		}
		for _, other := range app.Before {
			predecessors[other]++
			successors[app.Name] = append(successors[app.Name], other)
		}
	}

	// list of apps without predecessors (no incoming edges)
	queue := make([]string, 0, len(apps))
	for _, app := range apps {
		if predecessors[app.Name] == 0 {
			queue = append(queue, app.Name)
		}
	}

	// Kahn:
	//
	// Apps without predecessors are 'top' nodes. On each iteration, take
	// the next 'top' node, and decrease the predecessor count of each
	// successor app. Once that successor app has no more predecessors, take
	// it out of the predecessors set and add it to the queue of 'top'
	// nodes.
	for len(queue) > 0 {
		app := queue[0]
		queue = queue[1:]
		for _, successor := range successors[app] {
			predecessors[successor] -= 1
			if predecessors[successor] == 0 {
				delete(predecessors, successor)
				queue = append(queue, successor)
			}
		}
	}

	if len(predecessors) != 0 {
		// apps with predecessors unaccounted for are a part of
		// dependency cycle
		unsatisifed := bytes.Buffer{}
		for name := range predecessors {
			if unsatisifed.Len() > 0 {
				unsatisifed.WriteString(", ")
			}
			unsatisifed.WriteString(name)
		}
		return fmt.Errorf("applications are part of a before/after cycle: %s", unsatisifed.String())
	}
	return nil
}

func validateAppOrderNames(app *AppInfo, dependencies []string) error {
	// we must be a service to request ordering
	if len(dependencies) > 0 && !app.IsService() {
		return fmt.Errorf("cannot define before/after in application %q as it's not a service", app.Name)
	}

	for _, dep := range dependencies {
		// dependency is not defined
		other, ok := app.Snap.Apps[dep]
		if !ok {
			return fmt.Errorf("application %q refers to missing application %q in before/after",
				app.Name, dep)
		}

		if !other.IsService() {
			return fmt.Errorf("application %q refers to non-service application %q in before/after",
				app.Name, dep)
		}
	}
	return nil
}

// appContentWhitelist is the whitelist of legal chars in the "apps"
// section of snap.yaml. Do not allow any of [',",`] here or snap-exec
// will get confused.
var appContentWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:$-]*$`)

func ValidAppName(n string) bool {
	var validAppName = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

	return validAppName.MatchString(n)
}

// ValidateApp verifies the content in the app info.
func ValidateApp(app *AppInfo) error {
	switch app.Daemon {
	case "", "simple", "forking", "oneshot", "dbus", "notify":
		// valid
	default:
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	// Validate app name
	if !ValidAppName(app.Name) {
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

	if err := validateAppOrderNames(app, app.Before); err != nil {
		return err
	}
	if err := validateAppOrderNames(app, app.After); err != nil {
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

func isAbsAndClean(path string) bool {
	return (filepath.IsAbs(path) || strings.HasPrefix(path, "$")) && filepath.Clean(path) == path
}

// ValidateLayout ensures that the given layout contains only valid subset of constructs.
func ValidateLayout(layout *Layout) error {
	mountPoint := layout.Path

	if mountPoint == "" {
		return fmt.Errorf("layout cannot use an empty path")
	}

	if err := ValidatePathVariables(mountPoint); err != nil {
		return fmt.Errorf("layout %q uses invalid mount point: %s", layout.Path, err)
	}
	if !isAbsAndClean(mountPoint) {
		return fmt.Errorf("layout %q uses invalid mount point: must be absolute and clean", layout.Path)
	}

	var nused int
	if layout.Bind != "" {
		nused += 1
	}
	if layout.Type != "" {
		nused += 1
	}
	if layout.Symlink != "" {
		nused += 1
	}
	if nused != 1 {
		return fmt.Errorf("layout %q must define a bind mount, a filesystem mount or a symlink", layout.Path)
	}

	if layout.Bind != "" {
		mountSource := layout.Bind
		if err := ValidatePathVariables(mountSource); err != nil {
			return fmt.Errorf("layout %q uses invalid bind mount source %q: %s", layout.Path, mountSource, err)
		}
		if !isAbsAndClean(mountSource) {
			return fmt.Errorf("layout %q uses invalid bind mount source %q: must be absolute and clean", layout.Path, mountSource)
		}
	}

	switch layout.Type {
	case "tmpfs":
	case "":
		// nothing to do
	default:
		return fmt.Errorf("layout %q uses invalid filesystem %q", layout.Path, layout.Type)
	}

	if layout.Symlink != "" {
		oldname := layout.Symlink
		if err := ValidatePathVariables(oldname); err != nil {
			return fmt.Errorf("layout %q uses invalid symlink old name %q: %s", layout.Path, oldname, err)
		}
		if !isAbsAndClean(oldname) {
			return fmt.Errorf("layout %q uses invalid symlink old name %q: must be absolute and clean", layout.Path, oldname)
		}
	}

	// When new users and groups are supported those must be added to interfaces/mount/spec.go as well.
	// For now only "root" is allowed (and default).

	switch layout.User {
	case "root", "":
	// TODO: allow declared snap user and group names.
	default:
		return fmt.Errorf("layout %q uses invalid user %q", layout.Path, layout.User)
	}
	switch layout.Group {
	case "root", "":
	default:
		return fmt.Errorf("layout %q uses invalid group %q", layout.Path, layout.Group)
	}

	if layout.Mode&01777 != layout.Mode {
		return fmt.Errorf("layout %q uses invalid mode %#o", layout.Path, layout.Mode)
	}
	return nil
}
