// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeout"
)

// PlaceInfo offers all the information about where a snap and its data are located and exposed in the filesystem.
type PlaceInfo interface {
	// InstanceName returns the name of the snap.
	InstanceName() string

	// StoreName returns the name of the snap as referred to in the store.
	StoreName() string

	// InstanceMountDir returns the base directory of the snap.
	InstanceMountDir() string

	// InstanceMountFile returns the path where the snap file that is mounted is installed.
	InstanceMountFile() string

	// InstanceHooksDir returns the directory containing the snap's hooks.
	InstanceHooksDir() string

	// InstanceDataDir returns the data directory of the snap.
	InstanceDataDir() string

	// InstanceUserDataDir returns the per user data directory of the snap.
	InstanceUserDataDir(home string) string

	// InstanceCommonDataDir returns the data directory common across revisions of the snap.
	InstanceCommonDataDir() string

	// InstanceUserCommonDataDir returns the per user data directory common across revisions of the snap.
	InstanceUserCommonDataDir(home string) string

	// InstanceUserXdgRuntimeDir returns the per user XDG_RUNTIME_DIR directory
	InstanceUserXdgRuntimeDir(userID sys.UserID) string

	// InstanceDataHomeDir returns the a glob that matches all per user data directories of a snap.
	InstanceDataHomeDir() string

	// InstanceCommonDataHomeDir returns a glob that matches all per user data directories common across revisions of the snap.
	InstanceCommonDataHomeDir() string

	// InstanceXdgRuntimeDirs returns a glob that matches all XDG_RUNTIME_DIR directories for all users of the snap.
	InstanceXdgRuntimeDirs() string
}

// MinimalPlaceInfo returns a PlaceInfo with just the location information for a snap of the given name and revision.
func MinimalPlaceInfo(name string, revision Revision) PlaceInfo {
	// TODO consider using instance key
	return &Info{SideInfo: SideInfo{RealName: name, Revision: revision}}
}

// MountDir returns the base directory where it gets mounted of the snap with the given name and revision.
func MountDir(name string, revision Revision) string {
	return filepath.Join(dirs.SnapMountDir, name, revision.String())
}

// MountFile returns the path where the snap file that is mounted is installed.
func MountFile(name string, revision Revision) string {
	return filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%s.snap", name, revision))
}

// ScopedSecurityTag returns the snap-specific, scope specific, security tag.
func ScopedSecurityTag(snapName, scopeName, suffix string) string {
	return fmt.Sprintf("snap.%s.%s.%s", snapName, scopeName, suffix)
}

// SecurityTag returns the snap-specific security tag.
func SecurityTag(snapName string) string {
	return fmt.Sprintf("snap.%s", snapName)
}

// AppSecurityTag returns the application-specific security tag.
func AppSecurityTag(snapName, appName string) string {
	return fmt.Sprintf("%s.%s", SecurityTag(snapName), appName)
}

// HookSecurityTag returns the hook-specific security tag.
func HookSecurityTag(snapName, hookName string) string {
	return ScopedSecurityTag(snapName, "hook", hookName)
}

// NoneSecurityTag returns the security tag for interfaces that
// are not associated to an app or hook in the snap.
func NoneSecurityTag(snapName, uniqueName string) string {
	return ScopedSecurityTag(snapName, "none", uniqueName)
}

// DataDir returns the data directory of the snap.
func DataDir(snapName string, revision Revision) string {
	return filepath.Join(dirs.SnapDataDir, snapName, revision.String())
}

// CommonDataDir returns the data directory common across revisions of the snap.
func CommonDataDir(snapName string) string {
	return filepath.Join(dirs.SnapDataDir, snapName, "common")
}

// HooksDir returns the directory containing the snap's hooks.
func HooksDir(snapName string, revision Revision) string {
	return filepath.Join(MountDir(snapName, revision), "meta", "hooks")
}

// UserDataDir returns the user-specific data directory of the snap.
func UserDataDir(home string, snapName string, revision Revision) string {
	return filepath.Join(home, dirs.UserHomeSnapDir, snapName, revision.String())
}

// UserCommonDataDir returns the user-specific data directory common across revision of the snap.
func UserCommonDataDir(home string, snapName string) string {
	return filepath.Join(home, dirs.UserHomeSnapDir, snapName, "common")
}

// UserXdgRuntimeDir returns the XDG_RUNTIME_DIR directory of the snap for a particular user.
func UserXdgRuntimeDir(euid sys.UserID, snapName string) string {
	return filepath.Join("/run/user", fmt.Sprintf("%d/snap.%s", euid, snapName))
}

// SideInfo holds snap metadata that is crucial for the tracking of
// snaps and for the working of the system offline and which is not
// included in snap.yaml or for which the store is the canonical
// source overriding snap.yaml content.
//
// It can be marshalled and will be stored in the system state for
// each currently installed snap revision so it needs to be evolved
// carefully.
//
// Information that can be taken directly from snap.yaml or that comes
// from the store but is not required for working offline should not
// end up in SideInfo.
type SideInfo struct {
	RealName          string   `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string   `yaml:"snap-id" json:"snap-id"`
	Revision          Revision `yaml:"revision" json:"revision"`
	Channel           string   `yaml:"channel,omitempty" json:"channel,omitempty"`
	Contact           string   `yaml:"contact,omitempty" json:"contact,omitempty"`
	EditedTitle       string   `yaml:"title,omitempty" json:"title,omitempty"`
	EditedSummary     string   `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string   `yaml:"description,omitempty" json:"description,omitempty"`
	Private           bool     `yaml:"private,omitempty" json:"private,omitempty"`
	Paid              bool     `yaml:"paid,omitempty" json:"paid,omitempty"`
	InstanceKey       string   `yaml:"instance-key,omitempty" json:"instance-key,omitempty"`
}

// Info provides information about snaps.
type Info struct {
	SuggestedName string
	Version       string
	Type          Type
	Architectures []string
	Assumes       []string

	OriginalTitle       string
	OriginalSummary     string
	OriginalDescription string

	Environment strutil.OrderedMap

	LicenseAgreement string
	LicenseVersion   string
	License          string
	Epoch            Epoch
	Base             string
	Confinement      ConfinementType
	Apps             map[string]*AppInfo
	LegacyAliases    map[string]*AppInfo // FIXME: eventually drop this
	Hooks            map[string]*HookInfo
	Plugs            map[string]*PlugInfo
	Slots            map[string]*SlotInfo

	// Plugs or slots with issues (they are not included in Plugs or Slots)
	BadInterfaces map[string]string // slot or plug => message

	// The information in all the remaining fields is not sourced from the snap blob itself.
	SideInfo

	// Broken marks whether the snap is broken and the reason.
	Broken string

	// The information in these fields is ephemeral, available only from the store.
	DownloadInfo

	IconURL string
	Prices  map[string]float64
	MustBuy bool

	PublisherID string
	Publisher   string

	Screenshots []ScreenshotInfo

	// The flattended channel map with $track/$risk
	Channels map[string]*ChannelSnapInfo

	// The ordered list of tracks that contain channels
	Tracks []string

	Layout map[string]*Layout

	// The list of common-ids from all apps of the snap
	CommonIDs []string
}

// Layout describes a single element of the layout section.
type Layout struct {
	Snap *Info

	Path     string      `json:"path"`
	Bind     string      `json:"bind,omitempty"`
	BindFile string      `json:"bind-file,omitempty"`
	Type     string      `json:"type,omitempty"`
	User     string      `json:"user,omitempty"`
	Group    string      `json:"group,omitempty"`
	Mode     os.FileMode `json:"mode,omitempty"`
	Symlink  string      `json:"symlink,omitempty"`
}

// String returns a simple textual representation of a layout.
func (l *Layout) String() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s: ", l.Path)
	switch {
	case l.Bind != "":
		fmt.Fprintf(&buf, "bind %s", l.Bind)
	case l.BindFile != "":
		fmt.Fprintf(&buf, "bind-file %s", l.BindFile)
	case l.Symlink != "":
		fmt.Fprintf(&buf, "symlink %s", l.Symlink)
	case l.Type != "":
		fmt.Fprintf(&buf, "type %s", l.Type)
	default:
		fmt.Fprintf(&buf, "???")
	}
	if l.User != "root" && l.User != "" {
		fmt.Fprintf(&buf, ", user: %s", l.User)
	}
	if l.Group != "root" && l.Group != "" {
		fmt.Fprintf(&buf, ", group: %s", l.Group)
	}
	if l.Mode != 0755 {
		fmt.Fprintf(&buf, ", mode: %#o", l.Mode)
	}
	return buf.String()
}

// ChannelSnapInfo is the minimum information that can be used to clearly
// distinguish different revisions of the same snap.
type ChannelSnapInfo struct {
	Revision    Revision        `json:"revision"`
	Confinement ConfinementType `json:"confinement"`
	Version     string          `json:"version"`
	Channel     string          `json:"channel"`
	Epoch       Epoch           `json:"epoch"`
	Size        int64           `json:"size"`
}

// InstanceName returns the name of the locally installed snap.
func (s *Info) InstanceName() string {
	return InstanceName(s.StoreName(), s.InstanceKey)
}

// StoreName returns the the blessed name for the snap., name by which the snap
// is referred to in the store.
func (s *Info) StoreName() string {
	if s.RealName != "" {
		return s.RealName
	}
	return s.SuggestedName
}

// Title returns the blessed title for the snap.
func (s *Info) Title() string {
	if s.EditedTitle != "" {
		return s.EditedTitle
	}
	return s.OriginalTitle
}

// Summary returns the blessed summary for the snap.
func (s *Info) Summary() string {
	if s.EditedSummary != "" {
		return s.EditedSummary
	}
	return s.OriginalSummary
}

// Description returns the blessed description for the snap.
func (s *Info) Description() string {
	if s.EditedDescription != "" {
		return s.EditedDescription
	}
	return s.OriginalDescription
}

// InstanceMountDir returns the base directory of the snap where it gets mounted.
func (s *Info) InstanceMountDir() string {
	return MountDir(s.InstanceName(), s.Revision)
}

// InstanceMountFile returns the path where the snap file that is mounted is installed.
func (s *Info) InstanceMountFile() string {
	return MountFile(s.InstanceName(), s.Revision)
}

// InstanceHooksDir returns the directory containing the snap's hooks.
func (s *Info) InstanceHooksDir() string {
	return HooksDir(s.InstanceName(), s.Revision)
}

// InstanceDataDir returns the data directory of the snap.
func (s *Info) InstanceDataDir() string {
	return DataDir(s.InstanceName(), s.Revision)
}

// InstanceUserDataDir returns the user-specific data directory of the snap.
func (s *Info) InstanceUserDataDir(home string) string {
	return UserDataDir(home, s.InstanceName(), s.Revision)
}

// InstanceUserCommonDataDir returns the user-specific data directory common across revision of the snap.
func (s *Info) InstanceUserCommonDataDir(home string) string {
	return UserCommonDataDir(home, s.InstanceName())
}

// InstanceCommonDataDir returns the data directory common across revisions of the snap.
func (s *Info) InstanceCommonDataDir() string {
	return CommonDataDir(s.InstanceName())
}

// InstanceDataHomeDir returns the per user data directory of the snap.
func (s *Info) InstanceDataHomeDir() string {
	return filepath.Join(dirs.SnapDataHomeGlob, s.InstanceName(), s.Revision.String())
}

// InstanceCommonDataHomeDir returns the per user data directory common across revisions of the snap.
func (s *Info) InstanceCommonDataHomeDir() string {
	return filepath.Join(dirs.SnapDataHomeGlob, s.InstanceName(), "common")
}

// InstanceUserXdgRuntimeDir returns the XDG_RUNTIME_DIR directory of the snap for a particular user.
func (s *Info) InstanceUserXdgRuntimeDir(euid sys.UserID) string {
	return UserXdgRuntimeDir(euid, s.InstanceName())
}

// InstanceXdgRuntimeDirs returns the XDG_RUNTIME_DIR directories for all users of the snap.
func (s *Info) InstanceXdgRuntimeDirs() string {
	return filepath.Join(dirs.XdgRuntimeDirGlob, fmt.Sprintf("snap.%s", s.InstanceName()))
}

// NeedsDevMode returns whether the snap needs devmode.
func (s *Info) NeedsDevMode() bool {
	return s.Confinement == DevModeConfinement
}

// NeedsClassic  returns whether the snap needs classic confinement consent.
func (s *Info) NeedsClassic() bool {
	return s.Confinement == ClassicConfinement
}

// Services returns a list of the apps that have "daemon" set.
func (s *Info) Services() []*AppInfo {
	svcs := make([]*AppInfo, 0, len(s.Apps))
	for _, app := range s.Apps {
		if !app.IsService() {
			continue
		}
		svcs = append(svcs, app)
	}

	return svcs
}

func (s *Info) expandSnapVariables(path string, inMountNs bool) string {
	var name, dataDir, commonDataDir string

	name = s.InstanceName()
	if inMountNs {
		name = s.StoreName()
	}
	dataDir = DataDir(name, s.Revision)
	commonDataDir = CommonDataDir(name)

	return os.Expand(path, func(v string) string {
		switch v {
		case "SNAP":
			// TODO: account for non in-ns expansion?
			// NOTE: We use dirs.CoreSnapMountDir and s.StoreName()
			// here as the path used will be always inside the mount
			// namespace snap-confine creates and there we will
			// always have a /snap directory available regardless if
			// the system we're running on supports this or not and
			// the snap will be referred to using its store name.
			return filepath.Join(dirs.CoreSnapMountDir, s.StoreName(), s.Revision.String())
		case "SNAP_DATA":
			return dataDir
		case "SNAP_COMMON":
			return commonDataDir
		}
		return ""
	})
}

// ExpandSnapInstanceVariables resolves $SNAP, $SNAP_DATA and $SNAP_COMMON.
func (s *Info) ExpandSnapInstanceVariables(path string) string {
	return s.expandSnapVariables(path, false)
}

// ExpandSnapVariables resolves $SNAP, $SNAP_DATA and $SNAP_COMMON.
// TODO: those are expanded inside snap mount namespace
func (s *Info) ExpandSnapVariables(path string) string {
	return s.expandSnapVariables(path, true)
}

// InstallDate returns the "install date" of the snap.
//
// If the snap is not active, it'll return a zero time; otherwise
// it'll return the modtime of the "current" symlink. Sneaky.
func (s *Info) InstallDate() time.Time {
	dir, rev := filepath.Split(s.InstanceMountDir())
	cur := filepath.Join(dir, "current")
	tag, err := os.Readlink(cur)
	if err == nil && tag == rev {
		if st, err := os.Lstat(cur); err == nil {
			return st.ModTime()
		}
	}
	return time.Time{}
}

// BadInterfacesSummary returns a summary of the problems of bad plugs
// and slots in the snap.
func BadInterfacesSummary(snapInfo *Info) string {
	inverted := make(map[string][]string)
	for name, reason := range snapInfo.BadInterfaces {
		inverted[reason] = append(inverted[reason], name)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "snap %q has bad plugs or slots: ", snapInfo.InstanceName())
	reasons := make([]string, 0, len(inverted))
	for reason := range inverted {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		names := inverted[reason]
		sort.Strings(names)
		for i, name := range names {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(name)
		}
		fmt.Fprintf(&buf, " (%s); ", reason)
	}
	return strings.TrimSuffix(buf.String(), "; ")
}

// DownloadInfo contains the information to download a snap.
// It can be marshalled.
type DownloadInfo struct {
	AnonDownloadURL string `json:"anon-download-url,omitempty"`
	DownloadURL     string `json:"download-url,omitempty"`

	Size     int64  `json:"size,omitempty"`
	Sha3_384 string `json:"sha3-384,omitempty"`

	// The server can include information about available deltas for a given
	// snap at a specific revision during refresh. Currently during refresh the
	// server will provide single matching deltas only, from the clients
	// revision to the target revision when available, per requested format.
	Deltas []DeltaInfo `json:"deltas,omitempty"`
}

// DeltaInfo contains the information to download a delta
// from one revision to another.
type DeltaInfo struct {
	FromRevision    int    `json:"from-revision,omitempty"`
	ToRevision      int    `json:"to-revision,omitempty"`
	Format          string `json:"format,omitempty"`
	AnonDownloadURL string `json:"anon-download-url,omitempty"`
	DownloadURL     string `json:"download-url,omitempty"`
	Size            int64  `json:"size,omitempty"`
	Sha3_384        string `json:"sha3-384,omitempty"`
}

// sanity check that Info is a PlaceInfo
var _ PlaceInfo = (*Info)(nil)

// PlugInfo provides information about a plug.
type PlugInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
	Hooks     map[string]*HookInfo
}

func lookupAttr(attrs map[string]interface{}, path string) (interface{}, bool) {
	var v interface{}
	comps := strings.FieldsFunc(path, func(r rune) bool { return r == '.' })
	if len(comps) == 0 {
		return nil, false
	}
	v = attrs
	for _, comp := range comps {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, ok = m[comp]
		if !ok {
			return nil, false
		}
	}

	return v, true
}

func getAttribute(snapName string, ifaceName string, attrs map[string]interface{}, key string, val interface{}) error {
	v, ok := lookupAttr(attrs, key)
	if !ok {
		return fmt.Errorf("snap %q does not have attribute %q for interface %q", snapName, key, ifaceName)
	}

	rt := reflect.TypeOf(val)
	if rt.Kind() != reflect.Ptr || val == nil {
		return fmt.Errorf("internal error: cannot get %q attribute of interface %q with non-pointer value", key, ifaceName)
	}

	if reflect.TypeOf(v) != rt.Elem() {
		return fmt.Errorf("snap %q has interface %q with invalid value type for %q attribute", snapName, ifaceName, key)
	}
	rv := reflect.ValueOf(val)
	rv.Elem().Set(reflect.ValueOf(v))

	return nil
}

func (plug *PlugInfo) Attr(key string, val interface{}) error {
	return getAttribute(plug.Snap.InstanceName(), plug.Interface, plug.Attrs, key, val)
}

func (plug *PlugInfo) Lookup(key string) (interface{}, bool) {
	return lookupAttr(plug.Attrs, key)
}

// SecurityTags returns security tags associated with a given plug.
func (plug *PlugInfo) SecurityTags() []string {
	tags := make([]string, 0, len(plug.Apps)+len(plug.Hooks))
	for _, app := range plug.Apps {
		tags = append(tags, app.SecurityTag())
	}
	for _, hook := range plug.Hooks {
		tags = append(tags, hook.SecurityTag())
	}
	sort.Strings(tags)
	return tags
}

// String returns the representation of the plug as snap:plug string.
func (plug *PlugInfo) String() string {
	return fmt.Sprintf("%s:%s", plug.Snap.InstanceName(), plug.Name)
}

func (slot *SlotInfo) Attr(key string, val interface{}) error {
	return getAttribute(slot.Snap.InstanceName(), slot.Interface, slot.Attrs, key, val)
}

func (slot *SlotInfo) Lookup(key string) (interface{}, bool) {
	return lookupAttr(slot.Attrs, key)
}

// SecurityTags returns security tags associated with a given slot.
func (slot *SlotInfo) SecurityTags() []string {
	tags := make([]string, 0, len(slot.Apps))
	for _, app := range slot.Apps {
		tags = append(tags, app.SecurityTag())
	}
	for _, hook := range slot.Hooks {
		tags = append(tags, hook.SecurityTag())
	}
	sort.Strings(tags)
	return tags
}

// String returns the representation of the slot as snap:slot string.
func (slot *SlotInfo) String() string {
	return fmt.Sprintf("%s:%s", slot.Snap.InstanceName(), slot.Name)
}

// SlotInfo provides information about a slot.
type SlotInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
	Hooks     map[string]*HookInfo
}

// SocketInfo provides information on application sockets.
type SocketInfo struct {
	App *AppInfo

	Name         string
	ListenStream string
	SocketMode   os.FileMode
}

// TimerInfo provides information on application timer.
type TimerInfo struct {
	App *AppInfo

	Timer string
}

// StopModeType is the type for the "stop-mode:" of a snap app
type StopModeType string

// KillAll returns if the stop-mode means all processes should be killed
// when the service is stopped or just the main process.
func (st StopModeType) KillAll() bool {
	return string(st) == "" || strings.HasSuffix(string(st), "-all")
}

// KillSignal returns the signal that should be used to kill the process
// (or an empty string if no signal is needed)
func (st StopModeType) KillSignal() string {
	if st.Validate() != nil || st == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSuffix(string(st), "-all"))
}

func (st StopModeType) Validate() error {
	switch st {
	case "", "sigterm", "sigterm-all", "sighup", "sighup-all", "sigusr1", "sigusr1-all", "sigusr2", "sigusr2-all":
		// valid
		return nil
	}
	return fmt.Errorf(`"stop-mode" field contains invalid value %q`, st)
}

// AppInfo provides information about an app.
type AppInfo struct {
	Snap *Info

	Name          string
	LegacyAliases []string // FIXME: eventually drop this
	Command       string
	CommonID      string

	Daemon          string
	StopTimeout     timeout.Timeout
	WatchdogTimeout timeout.Timeout
	StopCommand     string
	ReloadCommand   string
	PostStopCommand string
	RestartCond     RestartCondition
	Completer       string
	RefreshMode     string
	StopMode        StopModeType

	// TODO: this should go away once we have more plumbing and can change
	// things vs refactor
	// https://github.com/snapcore/snapd/pull/794#discussion_r58688496
	BusName string

	Plugs   map[string]*PlugInfo
	Slots   map[string]*SlotInfo
	Sockets map[string]*SocketInfo

	Environment strutil.OrderedMap

	// list of other service names that this service will start after or
	// before
	After  []string
	Before []string

	Timer *TimerInfo

	Autostart string
}

// ScreenshotInfo provides information about a screenshot.
type ScreenshotInfo struct {
	URL    string
	Width  int64
	Height int64
}

// HookInfo provides information about a hook.
type HookInfo struct {
	Snap *Info

	Name  string
	Plugs map[string]*PlugInfo
	Slots map[string]*SlotInfo
}

// File returns the path to the *.socket file
func (socket *SocketInfo) File() string {
	return filepath.Join(dirs.SnapServicesDir, socket.App.SecurityTag()+"."+socket.Name+".socket")
}

// File returns the path to the *.timer file
func (timer *TimerInfo) File() string {
	return filepath.Join(dirs.SnapServicesDir, timer.App.SecurityTag()+".timer")
}

func (app *AppInfo) String() string {
	return JoinSnapApp(app.Snap.InstanceName(), app.Name)
}

// SecurityTag returns application-specific security tag.
//
// Security tags are used by various security subsystems as "profile names" and
// sometimes also as a part of the file name.
func (app *AppInfo) SecurityTag() string {
	return AppSecurityTag(app.Snap.InstanceName(), app.Name)
}

// DesktopFile returns the path to the installed optional desktop file for the application.
func (app *AppInfo) DesktopFile() string {
	return filepath.Join(dirs.SnapDesktopFilesDir, fmt.Sprintf("%s_%s.desktop", app.Snap.InstanceName(), app.Name))
}

// WrapperPath returns the path to wrapper invoking the app binary.
func (app *AppInfo) WrapperPath() string {
	return filepath.Join(dirs.SnapBinariesDir, JoinSnapApp(app.Snap.InstanceName(), app.Name))
}

// CompleterPath returns the path to the completer snippet for the app binary.
func (app *AppInfo) CompleterPath() string {
	return filepath.Join(dirs.CompletersDir, JoinSnapApp(app.Snap.InstanceName(), app.Name))
}

func (app *AppInfo) launcherCommand(command string) string {
	if command != "" {
		command = " " + command
	}
	if app.Name == app.Snap.StoreName() {
		return fmt.Sprintf("/usr/bin/snap run%s %s", command, app.Name)
	}
	return fmt.Sprintf("/usr/bin/snap run%s %s.%s", command, app.Snap.InstanceName(), app.Name)
}

// LauncherCommand returns the launcher command line to use when invoking the app binary.
func (app *AppInfo) LauncherCommand() string {
	if app.Timer != nil {
		return app.launcherCommand(fmt.Sprintf("--timer=%q", app.Timer.Timer))
	}
	return app.launcherCommand("")
}

// LauncherStopCommand returns the launcher command line to use when invoking the app stop command binary.
func (app *AppInfo) LauncherStopCommand() string {
	return app.launcherCommand("--command=stop")
}

// LauncherReloadCommand returns the launcher command line to use when invoking the app stop command binary.
func (app *AppInfo) LauncherReloadCommand() string {
	return app.launcherCommand("--command=reload")
}

// LauncherPostStopCommand returns the launcher command line to use when invoking the app post-stop command binary.
func (app *AppInfo) LauncherPostStopCommand() string {
	return app.launcherCommand("--command=post-stop")
}

// ServiceName returns the systemd service name for the daemon app.
func (app *AppInfo) ServiceName() string {
	return app.SecurityTag() + ".service"
}

// ServiceFile returns the systemd service file path for the daemon app.
func (app *AppInfo) ServiceFile() string {
	return filepath.Join(dirs.SnapServicesDir, app.ServiceName())
}

// Env returns the app specific environment overrides
func (app *AppInfo) Env() []string {
	appEnv := app.Snap.Environment.Copy()
	for _, k := range app.Environment.Keys() {
		appEnv.Set(k, app.Environment.Get(k))
	}

	return envFromMap(appEnv)
}

// IsService returns whether app represents a daemon/service.
func (app *AppInfo) IsService() bool {
	return app.Daemon != ""
}

// SecurityTag returns the hook-specific security tag.
//
// Security tags are used by various security subsystems as "profile names" and
// sometimes also as a part of the file name.
func (hook *HookInfo) SecurityTag() string {
	return HookSecurityTag(hook.Snap.InstanceName(), hook.Name)
}

// Env returns the hook-specific environment overrides
func (hook *HookInfo) Env() []string {
	return envFromMap(hook.Snap.Environment.Copy())
}

func envFromMap(envMap *strutil.OrderedMap) []string {
	env := []string{}
	for _, k := range envMap.Keys() {
		env = append(env, fmt.Sprintf("%s=%s", k, envMap.Get(k)))
	}

	return env
}

func infoFromSnapYamlWithSideInfo(meta []byte, si *SideInfo) (*Info, error) {
	info, err := InfoFromSnapYaml(meta)
	if err != nil {
		return nil, err
	}

	if si != nil {
		info.SideInfo = *si
	}

	return info, nil
}

// BrokenSnapError describes an error that refers to a snap that warrants the "broken" note.
type BrokenSnapError interface {
	error
	Broken() string
}

type NotFoundError struct {
	Snap     string
	Revision Revision
	// Path encodes the path that triggered the not-found error.
	// It may refer to a file inside the snap or to the snap file itself.
	Path string
}

func (e NotFoundError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("cannot find installed snap %q at revision %s: missing file %s", e.Snap, e.Revision, e.Path)
	}
	return fmt.Sprintf("cannot find installed snap %q at revision %s", e.Snap, e.Revision)
}

func (e NotFoundError) Broken() string {
	return e.Error()
}

type invalidMetaError struct {
	Snap     string
	Revision Revision
	Msg      string
}

func (e invalidMetaError) Error() string {
	return fmt.Sprintf("cannot use installed snap %q at revision %s: %s", e.Snap, e.Revision, e.Msg)
}

func (e invalidMetaError) Broken() string {
	return e.Error()
}

func MockSanitizePlugsSlots(f func(snapInfo *Info)) (restore func()) {
	old := SanitizePlugsSlots
	SanitizePlugsSlots = f
	return func() { SanitizePlugsSlots = old }
}

var SanitizePlugsSlots = func(snapInfo *Info) {
	panic("SanitizePlugsSlots function not set")
}

// ReadInfo reads the snap information for the installed snap with the given name and given side-info.
func ReadInfo(name string, si *SideInfo) (*Info, error) {
	snapYamlFn := filepath.Join(MountDir(name, si.Revision), "meta", "snap.yaml")
	meta, err := ioutil.ReadFile(snapYamlFn)
	if os.IsNotExist(err) {
		return nil, &NotFoundError{Snap: name, Revision: si.Revision, Path: snapYamlFn}
	}
	if err != nil {
		return nil, err
	}

	info, err := infoFromSnapYamlWithSideInfo(meta, si)
	if err != nil {
		return nil, &invalidMetaError{Snap: name, Revision: si.Revision, Msg: err.Error()}
	}
	_, instanceKey := SplitInstanceName(name)

	if instanceKey != "" && info.Type != TypeApp {
		return nil, &invalidMetaError{Snap: name, Revision: si.Revision, Msg: "instance key not allowed for non-app snaps"}
	}
	info.InstanceKey = instanceKey

	mountFile := MountFile(name, si.Revision)
	st, err := os.Stat(mountFile)
	if os.IsNotExist(err) {
		// This can happen when "snap try" mode snap is moved around. The mount
		// is still in place (it's a bind mount, it doesn't care about the
		// source moving) but the symlink in /var/lib/snapd/snaps is now
		// dangling.
		return nil, &NotFoundError{Snap: name, Revision: si.Revision, Path: mountFile}
	}
	if err != nil {
		return nil, err
	}
	info.Size = st.Size()

	err = addImplicitHooks(info)
	if err != nil {
		return nil, &invalidMetaError{Snap: name, Revision: si.Revision, Msg: err.Error()}
	}

	return info, nil
}

// ReadCurrentInfo reads the snap information from the installed snap in 'current' revision
func ReadCurrentInfo(snapName string) (*Info, error) {
	curFn := filepath.Join(dirs.SnapMountDir, snapName, "current")
	realFn, err := os.Readlink(curFn)
	if err != nil {
		return nil, fmt.Errorf("cannot find current revision for snap %s: %s", snapName, err)
	}
	rev := filepath.Base(realFn)
	revision, err := ParseRevision(rev)
	if err != nil {
		return nil, fmt.Errorf("cannot read revision %s: %s", rev, err)
	}

	return ReadInfo(snapName, &SideInfo{Revision: revision})
}

// ReadInfoFromSnapFile reads the snap information from the given Container
// and completes it with the given side-info if this is not nil.
func ReadInfoFromSnapFile(snapf Container, si *SideInfo) (*Info, error) {
	meta, err := snapf.ReadFile("meta/snap.yaml")
	if err != nil {
		return nil, err
	}

	info, err := infoFromSnapYamlWithSideInfo(meta, si)
	if err != nil {
		return nil, err
	}

	info.Size, err = snapf.Size()
	if err != nil {
		return nil, err
	}

	err = addImplicitHooksFromContainer(info, snapf)
	if err != nil {
		return nil, err
	}

	err = Validate(info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// InstallDate returns the "install date" of the snap.
//
// If the snap is not active, it'll return a zero time; otherwise
// it'll return the modtime of the "current" symlink.
func InstallDate(name string) time.Time {
	cur := filepath.Join(dirs.SnapMountDir, name, "current")
	if st, err := os.Lstat(cur); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// SplitSnapApp will split a string of the form `snap.app` into
// the `snap` and the `app` part. It also deals with the special
// case of snapName == appName.
func SplitSnapApp(snapApp string) (snap, app string) {
	l := strings.SplitN(snapApp, ".", 2)
	if len(l) < 2 {
		return l[0], l[0]
	}
	return l[0], l[1]
}

// JoinSnapApp produces a full application wrapper name from the
// `snap` and the `app` part. It also deals with the special
// case of snapName == appName.
func JoinSnapApp(snap, app string) string {
	if snap == app {
		return app
	}
	return fmt.Sprintf("%s.%s", snap, app)
}

// UseNick returns the nickname for given snap name. If there is none, returns
// the original name.
func UseNick(snapName string) string {
	if snapName == "core" {
		return "system"
	}
	return snapName
}

// DropNick returns the snap name for given nickname. If there is none, returns
// the original name.
func DropNick(nick string) string {
	if nick == "system" {
		return "core"
	}
	return nick
}

// StoreName splits the maybe-local name and returns the store name of the snap.
func StoreName(name string) string {
	store, _ := SplitInstanceName(name)
	return store
}

// SplitName splits the maybe-local name and returns the store name and the
// local key.
func SplitInstanceName(name string) (store string, instanceKey string) {
	split := strings.SplitN(name, "_", 2)
	store = split[0]
	if len(split) > 1 {
		instanceKey = split[1]
	}
	return store, instanceKey
}

// InstanceName takes the store name and the local key and returns a local name of
// the snap.
func InstanceName(storeName string, instanceKey string) string {
	if instanceKey != "" {
		return fmt.Sprintf("%s_%s", storeName, instanceKey)
	}
	return storeName
}
