// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/spdx"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timeutil"
)

// ValidateInstanceName checks if a string can be used as a snap instance name.
func ValidateInstanceName(instanceName string) error {
	return naming.ValidateInstance(instanceName)
}

// ValidateName checks if a string can be used as a snap name.
func ValidateName(name string) error {
	return naming.ValidateSnap(name)
}

// ValidateDesktopPrefix checks if a string can be used as a desktop file
// prefix. A desktop prefix should be of the form 'snapname' or
// 'snapname+instance'.
func ValidateDesktopPrefix(prefix string) bool {
	tokens := strings.Split(prefix, "+")
	if len(tokens) == 0 || len(tokens) > 2 {
		return false
	}
	if err := ValidateName(tokens[0]); err != nil {
		return false
	}
	if len(tokens) == 2 {
		if err := ValidateInstanceName(tokens[1]); err != nil {
			return false
		}
	}
	return true
}

// ValidatePlugName checks if a string can be used as a slot name.
//
// Slot names and plug names within one snap must have unique names.
// This is not enforced by this function but is enforced by snap-level
// validation.
func ValidatePlugName(name string) error {
	return naming.ValidatePlug(name)
}

// ValidateSlotName checks if a string can be used as a slot name.
//
// Slot names and plug names within one snap must have unique names.
// This is not enforced by this function but is enforced by snap-level
// validation.
func ValidateSlotName(name string) error {
	return naming.ValidateSlot(name)
}

// ValidateInterfaceName checks if a string can be used as an interface name.
func ValidateInterfaceName(name string) error {
	return naming.ValidateInterface(name)
}

// NB keep this in sync with snapcraft and the review tools :-)
var isValidVersion = regexp.MustCompile("^[a-zA-Z0-9](?:[a-zA-Z0-9:.+~-]{0,30}[a-zA-Z0-9+~])?$").MatchString

var isNonGraphicalASCII = regexp.MustCompile("[^[:graph:]]").MatchString
var isInvalidFirstVersionChar = regexp.MustCompile("^[^a-zA-Z0-9]").MatchString
var isInvalidLastVersionChar = regexp.MustCompile("[^a-zA-Z0-9+~]$").MatchString
var invalidMiddleVersionChars = regexp.MustCompile("[^a-zA-Z0-9:.+~-]+").FindAllString

// ValidateVersion checks if a string is a valid snap version.
func ValidateVersion(version string) error {
	if !isValidVersion(version) {
		// maybe it was too short?
		if len(version) == 0 {
			return errors.New("invalid snap version: cannot be empty")
		}
		if isNonGraphicalASCII(version) {
			// note that while this way of quoting the version can produce ugly
			// output in some cases (e.g. if you're trying to set a version to
			// "hello😁", seeing “invalid version "hello😁"” could be clearer than
			// “invalid snap version "hello\U0001f601"”), in a lot of more
			// interesting cases you _need_ to have the thing that's not ASCII
			// pointed out: homoglyphs and near-homoglyphs are too hard to spot
			// otherwise. Take for example a version of "аерс". Or "v1.0‑x".
			return fmt.Errorf("invalid snap version %s: must be printable, non-whitespace ASCII",
				strconv.QuoteToASCII(version))
		}
		// now we know it's a non-empty ASCII string, we can get serious
		var reasons []string
		// ... too long?
		if len(version) > 32 {
			reasons = append(reasons, fmt.Sprintf("cannot be longer than 32 characters (got: %d)", len(version)))
		}
		// started with a symbol?
		if isInvalidFirstVersionChar(version) {
			// note that we can only say version[0] because we know it's ASCII :-)
			reasons = append(reasons, fmt.Sprintf("must start with an ASCII alphanumeric (and not %q)", version[0]))
		}
		if len(version) > 1 {
			if isInvalidLastVersionChar(version) {
				tpl := "must end with an ASCII alphanumeric or one of '+' or '~' (and not %q)"
				reasons = append(reasons, fmt.Sprintf(tpl, version[len(version)-1]))
			}
			if len(version) > 2 {
				if all := invalidMiddleVersionChars(version[1:len(version)-1], -1); len(all) > 0 {
					reasons = append(reasons, fmt.Sprintf("contains invalid characters: %s", strutil.Quoted(all)))
				}
			}
		}
		switch len(reasons) {
		case 0:
			// huh
			return fmt.Errorf("invalid snap version %q", version)
		case 1:
			return fmt.Errorf("invalid snap version %q: %s", version, reasons[0])
		default:
			reasons, last := reasons[:len(reasons)-1], reasons[len(reasons)-1]
			return fmt.Errorf("invalid snap version %q: %s, and %s", version, strings.Join(reasons, ", "), last)
		}
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
	if err := naming.ValidateHook(hook.Name); err != nil {
		return err
	}

	// Also validate the command chain
	for _, value := range hook.CommandChain {
		if !commandChainContentWhitelist.MatchString(value) {
			return fmt.Errorf("hook command-chain contains illegal %q (legal: '%s')", value, commandChainContentWhitelist)
		}
	}

	return nil
}

// ValidateAlias checks if a string can be used as an alias name.
func ValidateAlias(alias string) error {
	return naming.ValidateAlias(alias)
}

// validateSocketName checks if a string ca be used as a name for a socket (for
// socket activation).
func validateSocketName(name string) error {
	return naming.ValidateSocket(name)
}

// validateSocketmode checks that the socket mode is a valid file mode.
func validateSocketMode(mode os.FileMode) error {
	if mode > 0777 {
		return fmt.Errorf("cannot use mode: %04o", mode)
	}

	return nil
}

// validateSocketAddr checks that the value of socket addresses.
func validateSocketAddr(socket *SocketInfo, fieldName string, address string) error {
	if address == "" {
		return fmt.Errorf("%q is not defined", fieldName)
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
		return fmt.Errorf("invalid %q: %q should be written as %q", fieldName, path, clean)
	}

	switch socket.App.DaemonScope {
	case SystemDaemon:
		if !(strings.HasPrefix(path, "$SNAP_DATA/") || strings.HasPrefix(path, "$SNAP_COMMON/") || strings.HasPrefix(path, "$XDG_RUNTIME_DIR/")) {
			return fmt.Errorf(
				"invalid %q: system daemon sockets must have a prefix of $SNAP_DATA, $SNAP_COMMON or $XDG_RUNTIME_DIR", fieldName)
		}
	case UserDaemon:
		if !(strings.HasPrefix(path, "$SNAP_USER_DATA/") || strings.HasPrefix(path, "$SNAP_USER_COMMON/") || strings.HasPrefix(path, "$XDG_RUNTIME_DIR/")) {
			return fmt.Errorf(
				"invalid %q: user daemon sockets must have a prefix of $SNAP_USER_DATA, $SNAP_USER_COMMON, or $XDG_RUNTIME_DIR", fieldName)
		}
	default:
		return fmt.Errorf("invalid %q: cannot validate sockets for daemon-scope %q", fieldName, socket.App.DaemonScope)
	}

	return nil
}

func validateSocketAddrAbstract(socket *SocketInfo, fieldName string, path string) error {
	// this comes from snap declaration, so the prefix can only be the snap
	// name at this point
	prefix := fmt.Sprintf("@snap.%s.", socket.App.Snap.SnapName())
	if !strings.HasPrefix(path, prefix) {
		return fmt.Errorf("path for %q must be prefixed with %q", fieldName, prefix)
	}
	return nil
}

func validateSocketAddrNet(socket *SocketInfo, fieldName string, address string) error {
	lastIndex := strings.LastIndex(address, ":")
	if lastIndex >= 0 {
		if err := validateSocketAddrNetHost(fieldName, address[:lastIndex]); err != nil {
			return err
		}
		return validateSocketAddrNetPort(fieldName, address[lastIndex+1:])
	}

	// Address only contains a port
	return validateSocketAddrNetPort(fieldName, address)
}

func validateSocketAddrNetHost(fieldName string, address string) error {
	validAddresses := []string{"127.0.0.1", "[::1]", "[::]"}
	for _, valid := range validAddresses {
		if address == valid {
			return nil
		}
	}

	return fmt.Errorf("invalid %q address %q, must be one of: %s", fieldName, address, strings.Join(validAddresses, ", "))
}

func validateSocketAddrNetPort(fieldName string, port string) error {
	var val uint64
	var err error
	retErr := fmt.Errorf("invalid %q port number %q", fieldName, port)
	if val, err = strconv.ParseUint(port, 10, 16); err != nil {
		return retErr
	}
	if val < 1 || val > 65535 {
		return retErr
	}
	return nil
}

func validateDescription(descr string) error {
	if count := utf8.RuneCountInString(descr); count > 4096 {
		return fmt.Errorf("description can have up to 4096 codepoints, got %d", count)
	}
	return nil
}

func validateTitle(title string) error {
	if count := utf8.RuneCountInString(title); count > 40 {
		return fmt.Errorf("title can have up to 40 codepoints, got %d", count)
	}
	return nil
}

// Validate verifies the content in the info.
func Validate(info *Info) error {
	name := info.InstanceName()
	if name == "" {
		return errors.New("snap name cannot be empty")
	}

	if err := ValidateName(info.SnapName()); err != nil {
		return err
	}
	if err := ValidateInstanceName(name); err != nil {
		return err
	}

	if err := validateTitle(info.Title()); err != nil {
		return err
	}

	if err := validateDescription(info.Description()); err != nil {
		return err
	}

	if err := ValidateVersion(info.Version); err != nil {
		return err
	}

	if err := info.Epoch.Validate(); err != nil {
		return err
	}

	if license := info.License; license != "" {
		if err := ValidateLicense(license); err != nil {
			return err
		}
	}

	// validate app entries
	for _, app := range info.Apps {
		if err := ValidateApp(app); err != nil {
			return fmt.Errorf("invalid definition of application %q: %v", app.Name, err)
		}
	}

	// validate apps ordering according to after/before
	if err := validateAppOrderCycles(info.Services()); err != nil {
		return err
	}

	// validate aliases
	for alias, app := range info.LegacyAliases {
		if err := naming.ValidateAlias(alias); err != nil {
			return fmt.Errorf("cannot have %q as alias name for app %q - use only letters, digits, dash, underscore and dot characters", alias, app.Name)
		}
	}

	// validate hook entries
	for _, hook := range info.Hooks {
		if err := ValidateHook(hook); err != nil {
			return err
		}
	}

	// Ensure that plugs and slots have appropriate names and interface names.
	if err := plugsSlotsInterfacesNames(info); err != nil {
		return err
	}

	// Ensure that plug and slot have unique names.
	if err := plugsSlotsUniqueNames(info); err != nil {
		return err
	}

	// Ensure that base field is valid
	if err := ValidateBase(info); err != nil {
		return err
	}

	// Ensure system usernames are valid
	if err := ValidateSystemUsernames(info); err != nil {
		return err
	}

	// Ensure links are valid
	if err := ValidateLinks(info.Links()); err != nil {
		return err
	}

	// ensure that common-id(s) are unique
	if err := ValidateCommonIDs(info); err != nil {
		return err
	}

	return ValidateLayoutAll(info)
}

// ValidateBase validates the base field.
func ValidateBase(info *Info) error {
	// validate that bases do not have base fields
	if info.Type() == TypeOS || info.Type() == TypeBase {
		if info.Base != "" && info.Base != "none" {
			return fmt.Errorf(`cannot have "base" field on %q snap %q`, info.Type(), info.InstanceName())
		}
	}

	if info.Base == "none" && (len(info.Hooks) > 0 || len(info.Apps) > 0) {
		return fmt.Errorf(`cannot have apps or hooks with base "none"`)
	}

	if info.Base != "" {
		baseSnapName, instanceKey := SplitInstanceName(info.Base)
		if instanceKey != "" {
			return fmt.Errorf("base cannot specify a snap instance name: %q", info.Base)
		}
		if err := ValidateName(baseSnapName); err != nil {
			return fmt.Errorf("invalid base name: %s", err)
		}
	}
	return nil
}

// ValidateLayoutAll validates the consistency of all the layout elements in a snap.
func ValidateLayoutAll(info *Info) error {
	paths := make([]string, 0, len(info.Layout))
	for _, layout := range info.Layout {
		paths = append(paths, layout.Path)
	}
	sort.Strings(paths)

	// Validate that each source path is not a new top-level directory
	for _, layout := range info.Layout {
		cleanPathSrc := info.ExpandSnapVariables(filepath.Clean(layout.Path))
		elems := strings.SplitN(cleanPathSrc, string(os.PathSeparator), 3)
		switch len(elems) {
		// len(1) is either relative path or empty string, will be validated
		// elsewhere
		case 2, 3:
			// if the first string is the empty string, then we have a top-level
			// directory to check
			if elems[0] != "" {
				// not the empty string which means this was a relative
				// specification, i.e. usr/src/doc
				return fmt.Errorf("layout %q is a relative filename", layout.Path)
			}
			if elems[1] != "" {
				// verify that the top-level directory is a supported one
				// we can't create new top-level directories because that would
				// require creating a mimic on top of "/" which we don't
				// currently support
				switch elems[1] {
				// this list was produced by taking all of the top level
				// directories in the core snap and removing the explicitly
				// denied top-level directories
				case "bin", "etc", "lib", "lib64", "meta", "mnt", "opt", "root", "sbin", "snap", "srv", "usr", "var", "writable":
				default:
					return fmt.Errorf("layout %q defines a new top-level directory %q", layout.Path, "/"+elems[1])
				}
			}
		}
	}

	// Validate that each source path is used consistently as a file or as a directory.
	sourceKindMap := make(map[string]string)
	for _, path := range paths {
		layout := info.Layout[path]
		if layout.Bind != "" {
			// Layout refers to a directory.
			sourcePath := info.ExpandSnapVariables(layout.Bind)
			if kind, ok := sourceKindMap[sourcePath]; ok {
				if kind != "dir" {
					return fmt.Errorf("layout %q refers to directory %q but another layout treats it as file", layout.Path, layout.Bind)
				}
			}
			sourceKindMap[sourcePath] = "dir"
		}
		if layout.BindFile != "" {
			// Layout refers to a file.
			sourcePath := info.ExpandSnapVariables(layout.BindFile)
			if kind, ok := sourceKindMap[sourcePath]; ok {
				if kind != "file" {
					return fmt.Errorf("layout %q refers to file %q but another layout treats it as a directory", layout.Path, layout.BindFile)
				}
			}
			sourceKindMap[sourcePath] = "file"
		}
	}

	// Validate that layout are not attempting to define elements that normally
	// come from other snaps. This is separate from the ValidateLayout below to
	// simplify argument passing.
	thisSnapMntDir := filepath.Join("/snap/", info.SnapName())
	for _, path := range paths {
		if strings.HasPrefix(path, "/snap/") && !strings.HasPrefix(path, thisSnapMntDir) {
			return fmt.Errorf("layout %q defines a layout in space belonging to another snap", path)
		}
	}

	// Validate each layout item and collect resulting constraints.
	constraints := make([]LayoutConstraint, 0, len(info.Layout))
	for _, path := range paths {
		layout := info.Layout[path]
		if err := ValidateLayout(layout, constraints); err != nil {
			return err
		}
		constraints = append(constraints, layout.constraint())
	}
	return nil
}

func plugsSlotsInterfacesNames(info *Info) error {
	for plugName, plug := range info.Plugs {
		if err := ValidatePlugName(plugName); err != nil {
			return err
		}
		if err := ValidateInterfaceName(plug.Interface); err != nil {
			return fmt.Errorf("invalid interface name %q for plug %q", plug.Interface, plugName)
		}
	}
	for slotName, slot := range info.Slots {
		if err := ValidateSlotName(slotName); err != nil {
			return err
		}
		if err := ValidateInterfaceName(slot.Interface); err != nil {
			return fmt.Errorf("invalid interface name %q for slot %q", slot.Interface, slotName)
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
func validateAppOrderCycles(apps []*AppInfo) error {
	if _, err := SortServices(apps); err != nil {
		return err
	}
	return nil
}

func validateAppOrderNames(app *AppInfo, dependencies []string) error {
	// we must be a service to request ordering
	if len(dependencies) > 0 && !app.IsService() {
		return errors.New("must be a service to define before/after ordering")
	}

	for _, dep := range dependencies {
		// dependency is not defined
		other, ok := app.Snap.Apps[dep]
		if !ok {
			return fmt.Errorf("before/after references a missing application %q", dep)
		}

		if !other.IsService() {
			return fmt.Errorf("before/after references a non-service application %q", dep)
		}

		if app.DaemonScope != other.DaemonScope {
			return fmt.Errorf("before/after references service with different daemon-scope %q", dep)
		}
	}
	return nil
}

func validateAppTimeouts(app *AppInfo) error {
	type T struct {
		desc    string
		timeout timeout.Timeout
	}
	for _, t := range []T{
		{"start-timeout", app.StartTimeout},
		{"stop-timeout", app.StopTimeout},
		{"watchdog-timeout", app.WatchdogTimeout},
	} {
		if t.timeout == 0 {
			continue
		}
		if !app.IsService() {
			return fmt.Errorf("%s is only applicable to services", t.desc)
		}
		if t.timeout < 0 {
			return fmt.Errorf("%s cannot be negative", t.desc)
		}
	}
	return nil
}

func validateAppTimer(app *AppInfo) error {
	if app.Timer == nil {
		return nil
	}

	if !app.IsService() {
		return errors.New("timer is only applicable to services")
	}

	if _, err := timeutil.ParseSchedule(app.Timer.Timer); err != nil {
		return fmt.Errorf("timer has invalid format: %v", err)
	}

	return nil
}

func validateAppRestart(app *AppInfo) error {
	// app.RestartCond value is validated when unmarshalling

	if app.RestartDelay == 0 && app.RestartCond == "" {
		return nil
	}

	if app.RestartDelay != 0 {
		if !app.IsService() {
			return errors.New("restart-delay is only applicable to services")
		}

		if app.RestartDelay < 0 {
			return errors.New("restart-delay cannot be negative")
		}
	}

	if app.RestartCond != "" {
		if !app.IsService() {
			return errors.New("restart-condition is only applicable to services")
		}
	}
	return nil
}

func validateAppActivatesOn(app *AppInfo) error {
	if len(app.ActivatesOn) == 0 {
		return nil
	}

	if !app.IsService() {
		return errors.New("activates-on is only applicable to services")
	}

	for _, slot := range app.ActivatesOn {
		// ActivatesOn slots must use the "dbus" interface
		if slot.Interface != "dbus" {
			return fmt.Errorf("invalid activates-on value %q: slot does not use dbus interface", slot.Name)
		}

		// D-Bus slots must match the daemon scope
		bus := slot.Attrs["bus"]
		if app.DaemonScope == SystemDaemon && bus != "system" || app.DaemonScope == UserDaemon && bus != "session" {
			return fmt.Errorf("invalid activates-on value %q: bus %q does not match daemon-scope %q", slot.Name, bus, app.DaemonScope)
		}

		// Slots must only be activatable on a single app
		for _, otherApp := range slot.Apps {
			if otherApp == app {
				continue
			}
			for _, otherSlot := range otherApp.ActivatesOn {
				if otherSlot == slot {
					return fmt.Errorf("invalid activates-on value %q: slot is also activatable on app %q", slot.Name, otherApp.Name)
				}
			}
		}
	}

	return nil
}

// appContentWhitelist is the whitelist of legal chars in the "apps"
// section of snap.yaml. Do not allow any of [',",`] here or snap-exec
// will get confused. chainContentWhitelist is the same, but for the
// command-chain, which also doesn't allow whitespace.
var appContentWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:$-]*$`)
var commandChainContentWhitelist = regexp.MustCompile(`^[A-Za-z0-9/._#:$-]*$`)

// ValidAppName tells whether a string is a valid application name.
func ValidAppName(n string) bool {
	return naming.ValidateApp(n) == nil
}

// ValidateApp verifies the content in the app info.
func ValidateApp(app *AppInfo) error {
	switch app.Daemon {
	case "", "simple", "forking", "oneshot", "dbus", "notify":
		// valid
	default:
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	switch app.DaemonScope {
	case "":
		if app.Daemon != "" {
			return fmt.Errorf(`"daemon-scope" must be set for daemons`)
		}
	case SystemDaemon, UserDaemon:
		if app.Daemon == "" {
			return fmt.Errorf(`"daemon-scope" can only be set for daemons`)
		}
	default:
		return fmt.Errorf(`invalid "daemon-scope": %q`, app.DaemonScope)
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

	// Also validate the command chain
	for _, value := range app.CommandChain {
		if err := validateField("command-chain", value, commandChainContentWhitelist); err != nil {
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
		if err := validateAppSocket(socket); err != nil {
			return fmt.Errorf("invalid definition of socket %q: %v", socket.Name, err)
		}
	}

	if err := validateAppActivatesOn(app); err != nil {
		return err
	}

	if err := validateAppRestart(app); err != nil {
		return err
	}
	if err := validateAppOrderNames(app, app.Before); err != nil {
		return err
	}
	if err := validateAppOrderNames(app, app.After); err != nil {
		return err
	}

	if err := validateAppTimeouts(app); err != nil {
		return err
	}

	// validate stop-mode
	if err := app.StopMode.Validate(); err != nil {
		return err
	}
	// validate refresh-mode
	switch app.RefreshMode {
	case "", "endure", "restart":
		// valid
	default:
		return fmt.Errorf(`"refresh-mode" field contains invalid value %q`, app.RefreshMode)
	}
	// validate install-mode
	switch app.InstallMode {
	case "", "enable", "disable":
		// valid
	default:
		return fmt.Errorf(`"install-mode" field contains invalid value %q`, app.InstallMode)
	}
	if app.StopMode != "" && app.Daemon == "" {
		return fmt.Errorf(`"stop-mode" cannot be used for %q, only for services`, app.Name)
	}
	if app.RefreshMode != "" && app.Daemon == "" {
		return fmt.Errorf(`"refresh-mode" cannot be used for %q, only for services`, app.Name)
	}
	if app.InstallMode != "" && app.Daemon == "" {
		return fmt.Errorf(`"install-mode" cannot be used for %q, only for services`, app.Name)
	}

	return validateAppTimer(app)
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

// LayoutConstraint abstracts validation of conflicting layout elements.
type LayoutConstraint interface {
	IsOffLimits(path string) bool
}

// mountedTree represents a mounted file-system tree or a bind-mounted directory.
type mountedTree string

// IsOffLimits returns true if the mount point is (perhaps non-proper) prefix of a given path.
func (mountPoint mountedTree) IsOffLimits(path string) bool {
	return strings.HasPrefix(path, string(mountPoint)+"/") || path == string(mountPoint)
}

// mountedFile represents a bind-mounted file.
type mountedFile string

// IsOffLimits returns true if the mount point is (perhaps non-proper) prefix of a given path.
func (mountPoint mountedFile) IsOffLimits(path string) bool {
	return strings.HasPrefix(path, string(mountPoint)+"/") || path == string(mountPoint)
}

// symlinkFile represents a layout using symbolic link.
type symlinkFile string

// IsOffLimits returns true for mounted files  if a path is identical to the path of the mount point.
func (mountPoint symlinkFile) IsOffLimits(path string) bool {
	return strings.HasPrefix(path, string(mountPoint)+"/") || path == string(mountPoint)
}

func (layout *Layout) constraint() LayoutConstraint {
	path := layout.Snap.ExpandSnapVariables(layout.Path)
	if layout.Symlink != "" {
		return symlinkFile(path)
	} else if layout.BindFile != "" {
		return mountedFile(path)
	}
	return mountedTree(path)
}

// layoutRejectionList contains directories that cannot be used as layout
// targets. Nothing there, or underneath can be replaced with $SNAP or
// $SNAP_DATA, or $SNAP_COMMON content, even from the point of view of a single
// snap.
var layoutRejectionList = []string{
	// Special locations that need to retain their properties:

	// The /dev directory contains essential device nodes and there's no valid
	// reason to allow snaps to replace it.
	"/dev",
	// The /proc directory contains essential process meta-data and
	// miscellaneous kernel configuration parameters and there is no valid
	// reason to allow snaps to replace it.
	"/proc",
	// The /sys directory exposes many kernel internals, similar to /proc and
	// there is no known reason to allow snaps to replace it.
	"/sys",
	// The media directory is mounted with bi-directional mount event sharing.
	// Any mount operations there are reflected in the host's view of /media,
	// which may be either itself or /run/media.
	"/media",
	// The /run directory contains various ephemeral information files or
	// sockets used by various programs. Providing view of the true /run allows
	// snap applications to be integrated with the rest of the system and
	// therefore snaps should not be allowed to replace it.
	"/run",
	// The /tmp directory contains a private, per-snap, view of /tmp and
	// there's no valid reason to allow snaps to replace it.
	"/tmp",
	// The /var/lib/snapd directory contains essential snapd state and is
	// sometimes consulted from inside the mount namespace.
	"/var/lib/snapd",

	// Locations that may be used to attack the host:

	// The firmware is sometimes loaded on demand by the kernel, in response to
	// a process performing generic I/O to a specific device. In that case the
	// mount namespace of the process is searched, by the kernel, for the
	// firmware. Therefore firmware must not be replaceable to prevent
	// malicious firmware from attacking the host.
	"/lib/firmware",
	// Similarly the kernel will load modules and the modules should not be
	// something that snaps can tamper with.
	"/lib/modules",

	// Locations that store essential data:

	// The /var/snap directory contains system-wide state of particular snaps
	// and should not be replaced as it would break content interface
	// connections that use $SNAP_DATA or $SNAP_COMMON.
	"/var/snap",
	// The /home directory contains user data, including $SNAP_USER_DATA,
	// $SNAP_USER_COMMON and should be disallowed for the same reasons as
	// /var/snap.
	"/home",

	// Locations that should be pristine to avoid confusion.

	// There's no known reason to allow snaps to replace things there.
	"/boot",
	// The lost+found directory is used by fsck tools to link lost blocks back
	// into the filesystem tree. Using layouts for this element is just
	// confusing and there is no valid reason to allow it.
	"/lost+found",
}

// ValidateLayout ensures that the given layout contains only valid subset of constructs.
func ValidateLayout(layout *Layout, constraints []LayoutConstraint) error {
	si := layout.Snap
	// Rules for validating layouts:
	//
	// * source of mount --bind must be in on of $SNAP, $SNAP_DATA or $SNAP_COMMON
	// * target of symlink must in in one of $SNAP, $SNAP_DATA, or $SNAP_COMMON
	// * may not mount on top of an existing layout mountpoint

	mountPoint := layout.Path

	if mountPoint == "" {
		return errors.New("layout cannot use an empty path")
	}

	if err := ValidatePathVariables(mountPoint); err != nil {
		return fmt.Errorf("layout %q uses invalid mount point: %s", layout.Path, err)
	}
	mountPoint = si.ExpandSnapVariables(mountPoint)
	if !isAbsAndClean(mountPoint) {
		return fmt.Errorf("layout %q uses invalid mount point: must be absolute and clean", layout.Path)
	}

	for _, path := range layoutRejectionList {
		// We use the mountedTree constraint as this has the right semantics.
		if mountedTree(path).IsOffLimits(mountPoint) {
			return fmt.Errorf("layout %q in an off-limits area", layout.Path)
		}
	}

	for _, constraint := range constraints {
		if constraint.IsOffLimits(mountPoint) {
			return fmt.Errorf("layout %q underneath prior layout item %q", layout.Path, constraint)
		}
	}

	var nused int
	if layout.Bind != "" {
		nused++
	}
	if layout.BindFile != "" {
		nused++
	}
	if layout.Type != "" {
		nused++
	}
	if layout.Symlink != "" {
		nused++
	}
	if nused != 1 {
		return fmt.Errorf("layout %q must define a bind mount, a filesystem mount or a symlink", layout.Path)
	}

	if layout.Bind != "" || layout.BindFile != "" {
		mountSource := layout.Bind + layout.BindFile
		if err := ValidatePathVariables(mountSource); err != nil {
			return fmt.Errorf("layout %q uses invalid bind mount source %q: %s", layout.Path, mountSource, err)
		}
		mountSource = si.ExpandSnapVariables(mountSource)
		if !isAbsAndClean(mountSource) {
			return fmt.Errorf("layout %q uses invalid bind mount source %q: must be absolute and clean", layout.Path, mountSource)
		}
		// Bind mounts *must* use $SNAP, $SNAP_DATA or $SNAP_COMMON as bind
		// mount source. This is done so that snaps cannot bypass restrictions
		// by mounting something outside into their own space.
		if !strings.HasPrefix(mountSource, si.ExpandSnapVariables("$SNAP")) &&
			!strings.HasPrefix(mountSource, si.ExpandSnapVariables("$SNAP_DATA")) &&
			!strings.HasPrefix(mountSource, si.ExpandSnapVariables("$SNAP_COMMON")) {
			return fmt.Errorf("layout %q uses invalid bind mount source %q: must start with $SNAP, $SNAP_DATA or $SNAP_COMMON", layout.Path, mountSource)
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
		oldname = si.ExpandSnapVariables(oldname)
		if !isAbsAndClean(oldname) {
			return fmt.Errorf("layout %q uses invalid symlink old name %q: must be absolute and clean", layout.Path, oldname)
		}
		// Symlinks *must* use $SNAP, $SNAP_DATA or $SNAP_COMMON as oldname.
		// This is done so that snaps cannot attempt to bypass restrictions
		// by mounting something outside into their own space.
		if !strings.HasPrefix(oldname, si.ExpandSnapVariables("$SNAP")) &&
			!strings.HasPrefix(oldname, si.ExpandSnapVariables("$SNAP_DATA")) &&
			!strings.HasPrefix(oldname, si.ExpandSnapVariables("$SNAP_COMMON")) {
			return fmt.Errorf("layout %q uses invalid symlink old name %q: must start with $SNAP, $SNAP_DATA or $SNAP_COMMON", layout.Path, oldname)
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

func ValidateCommonIDs(info *Info) error {
	seen := make(map[string]string, len(info.Apps))
	for _, app := range info.Apps {
		if app.CommonID != "" {
			if other, was := seen[app.CommonID]; was {
				return fmt.Errorf("application %q common-id %q must be unique, already used by application %q",
					app.Name, app.CommonID, other)
			}
			seen[app.CommonID] = app.Name
		}
	}
	return nil
}

func ValidateSystemUsernames(info *Info) error {
	for username := range info.SystemUsernames {
		if !osutil.IsValidUsername(username) {
			return fmt.Errorf("invalid system username %q", username)
		}
	}
	return nil
}

// NeededDefaultProviders returns a map keyed by the names of all
// default-providers for the content plugs that the given snap.Info
// needs. The map values are the corresponding content tags.
func NeededDefaultProviders(info *Info) (providerSnapsToContentTag map[string][]string) {
	providerSnapsToContentTag = make(map[string][]string)
	for _, plug := range info.Plugs {
		gatherDefaultContentProvider(providerSnapsToContentTag, plug)
	}
	return providerSnapsToContentTag
}

// ValidateBasesAndProviders checks that all bases/default-providers are part of the seed
func ValidateBasesAndProviders(snapInfos []*Info) []error {
	all := naming.NewSnapSet(nil)
	for _, info := range snapInfos {
		all.Add(info)
	}

	var errs []error
	for _, info := range snapInfos {
		// ensure base is available
		if info.Base != "" && info.Base != "none" {
			if !all.Contains(naming.Snap(info.Base)) {
				errs = append(errs, fmt.Errorf("cannot use snap %q: base %q is missing", info.InstanceName(), info.Base))
			}
		}
		// ensure core is available
		if info.Base == "" && info.SnapType == TypeApp && info.InstanceName() != "snapd" {
			if !all.Contains(naming.Snap("core")) {
				errs = append(errs, fmt.Errorf(`cannot use snap %q: required snap "core" missing`, info.InstanceName()))
			}
		}
		// ensure default-providers are available
		for dp := range NeededDefaultProviders(info) {
			if !all.Contains(naming.Snap(dp)) {
				errs = append(errs, fmt.Errorf("cannot use snap %q: default provider %q is missing", info.InstanceName(), dp))
			}
		}
	}
	return errs
}

var isValidLinksKey = regexp.MustCompile("^[a-zA-Z](?:-?[a-zA-Z0-9])*$").MatchString
var validLinkSchemes = []string{"http", "https"}

// ValidateLinks checks that links entries have valid keys and values that can be parsed as URLs or are email addresses possibly prefixed with mailto:.
func ValidateLinks(links map[string][]string) error {
	for linksKey, linksValues := range links {
		if linksKey == "" {
			return fmt.Errorf("links key cannot be empty")
		}
		if !isValidLinksKey(linksKey) {
			return fmt.Errorf("links key is invalid: %s", linksKey)
		}
		if len(linksValues) == 0 {
			return fmt.Errorf("%q links cannot be specified and empty", linksKey)
		}
		for _, link := range linksValues {
			if link == "" {
				return fmt.Errorf("empty %q link", linksKey)
			}
			u, err := url.Parse(link)
			if err != nil {
				return fmt.Errorf("invalid %q link %q", linksKey, link)
			}
			if u.Scheme == "" || u.Scheme == "mailto" {
				// minimal check
				if !strings.Contains(link, "@") {
					return fmt.Errorf("invalid %q email address %q", linksKey, link)
				}
			} else if !strutil.ListContains(validLinkSchemes, u.Scheme) {
				return fmt.Errorf("%q link must have one of http|https schemes or it must be an email address: %q", linksKey, link)
			}
		}
	}
	return nil
}
