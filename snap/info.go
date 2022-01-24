// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeout"
)

// PlaceInfo offers all the information about where a snap and its data are
// located and exposed in the filesystem.
type PlaceInfo interface {
	// InstanceName returns the name of the snap decorated with instance
	// key, if any.
	InstanceName() string

	// SnapName returns the name of the snap.
	SnapName() string

	// SnapRevision returns the revision of the snap.
	SnapRevision() Revision

	// Filename returns the name of the snap with the revision
	// number, as used on the filesystem.
	Filename() string

	// MountDir returns the base directory of the snap.
	MountDir() string

	// MountFile returns the path where the snap file that is mounted is
	// installed.
	MountFile() string

	// HooksDir returns the directory containing the snap's hooks.
	HooksDir() string

	// DataDir returns the data directory of the snap.
	DataDir() string

	// UserDataDir returns the per user data directory of the snap.
	UserDataDir(home string, opts *dirs.SnapDirOptions) string

	// CommonDataDir returns the data directory common across revisions of the
	// snap.
	CommonDataDir() string

	// UserCommonDataDir returns the per user data directory common across
	// revisions of the snap.
	UserCommonDataDir(home string, opts *dirs.SnapDirOptions) string

	// UserXdgRuntimeDir returns the per user XDG_RUNTIME_DIR directory
	UserXdgRuntimeDir(userID sys.UserID) string

	// DataHomeDir returns a glob that matches all per user data directories
	// of a snap.
	DataHomeDir(opts *dirs.SnapDirOptions) string

	// CommonDataHomeDir returns a glob that matches all per user data
	// directories common across revisions of the snap.
	CommonDataHomeDir(opts *dirs.SnapDirOptions) string

	// XdgRuntimeDirs returns a glob that matches all XDG_RUNTIME_DIR
	// directories for all users of the snap.
	XdgRuntimeDirs() string
}

// MinimalPlaceInfo returns a PlaceInfo with just the location information for a
// snap of the given name and revision.
func MinimalPlaceInfo(name string, revision Revision) PlaceInfo {
	storeName, instanceKey := SplitInstanceName(name)
	return &Info{SideInfo: SideInfo{RealName: storeName, Revision: revision}, InstanceKey: instanceKey}
}

// ParsePlaceInfoFromSnapFileName returns a PlaceInfo with just the location
// information for a snap of file name, failing if the snap file name is invalid
// This explicitly does not support filenames with instance names in them
func ParsePlaceInfoFromSnapFileName(sn string) (PlaceInfo, error) {
	if sn == "" {
		return nil, fmt.Errorf("empty snap file name")
	}
	if strings.Count(sn, "_") > 1 {
		// too many "_", probably has an instance key in the filename like in
		// snap-name_key_23.snap
		return nil, fmt.Errorf("too many '_' in snap file name")
	}
	idx := strings.IndexByte(sn, '_')
	switch {
	case idx < 0:
		return nil, fmt.Errorf("snap file name %q has invalid format (missing '_')", sn)
	case idx == 0:
		return nil, fmt.Errorf("snap file name %q has invalid format (no snap name before '_')", sn)
	}
	// ensure that _ is not the last element
	name := sn[:idx]
	revnoNSuffix := sn[idx+1:]
	rev, err := ParseRevision(strings.TrimSuffix(revnoNSuffix, ".snap"))
	if err != nil {
		return nil, fmt.Errorf("cannot parse revision in snap file name %q: %v", sn, err)
	}
	return &Info{SideInfo: SideInfo{RealName: name, Revision: rev}}, nil
}

// BaseDir returns the system level directory of given snap.
func BaseDir(name string) string {
	return filepath.Join(dirs.SnapMountDir, name)
}

// MountDir returns the base directory where it gets mounted of the snap with
// the given name and revision.
func MountDir(name string, revision Revision) string {
	return filepath.Join(BaseDir(name), revision.String())
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

// BaseDataDir returns the base directory for snap data locations.
func BaseDataDir(name string) string {
	return filepath.Join(dirs.SnapDataDir, name)
}

// DataDir returns the data directory for given snap name and revision. The name
// can be
// either a snap name or snap instance name.
func DataDir(name string, revision Revision) string {
	return filepath.Join(BaseDataDir(name), revision.String())
}

// CommonDataDir returns the common data directory for given snap name. The name
// can be either a snap name or snap instance name.
func CommonDataDir(name string) string {
	return filepath.Join(dirs.SnapDataDir, name, "common")
}

// HooksDir returns the directory containing the snap's hooks for given snap
// name. The name can be either a snap name or snap instance name.
func HooksDir(name string, revision Revision) string {
	return filepath.Join(MountDir(name, revision), "meta", "hooks")
}

func snapDataDir(opts *dirs.SnapDirOptions) string {
	if opts == nil {
		opts = &dirs.SnapDirOptions{}
	}

	if opts.HiddenSnapDataDir {
		return dirs.HiddenSnapDataHomeDir
	}

	return dirs.UserHomeSnapDir
}

// UserDataDir returns the user-specific data directory for given snap name. The
// name can be either a snap name or snap instance name.
func UserDataDir(home string, name string, revision Revision, opts *dirs.SnapDirOptions) string {
	return filepath.Join(home, snapDataDir(opts), name, revision.String())
}

// UserCommonDataDir returns the user-specific common data directory for given
// snap name. The name can be either a snap name or snap instance name.
func UserCommonDataDir(home string, name string, opts *dirs.SnapDirOptions) string {
	return filepath.Join(home, snapDataDir(opts), name, "common")
}

// UserSnapDir returns the user-specific directory for given
// snap name. The name can be either a snap name or snap instance name.
func UserSnapDir(home string, name string, opts *dirs.SnapDirOptions) string {
	return filepath.Join(home, snapDataDir(opts), name)
}

// UserXdgRuntimeDir returns the user-specific XDG_RUNTIME_DIR directory for
// given snap name. The name can be either a snap name or snap instance name.
func UserXdgRuntimeDir(euid sys.UserID, name string) string {
	return filepath.Join(dirs.XdgRuntimeDirBase, fmt.Sprintf("%d/snap.%s", euid, name))
}

// SnapDir returns the user-specific snap directory.
func SnapDir(home string, opts *dirs.SnapDirOptions) string {
	return filepath.Join(home, snapDataDir(opts))
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
	RealName          string              `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string              `yaml:"snap-id" json:"snap-id"`
	Revision          Revision            `yaml:"revision" json:"revision"`
	Channel           string              `yaml:"channel,omitempty" json:"channel,omitempty"`
	EditedLinks       map[string][]string `yaml:"links,omitempty" json:"links,omitempty"`
	EditedContact     string              `yaml:"contact,omitempty" json:"contact,omitempty"`
	EditedTitle       string              `yaml:"title,omitempty" json:"title,omitempty"`
	EditedSummary     string              `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string              `yaml:"description,omitempty" json:"description,omitempty"`
	Private           bool                `yaml:"private,omitempty" json:"private,omitempty"`
	Paid              bool                `yaml:"paid,omitempty" json:"paid,omitempty"`
}

// Info provides information about snaps.
type Info struct {
	SuggestedName string
	InstanceKey   string
	Version       string
	SnapType      Type
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

	// The information in all the remaining fields is not sourced from the snap
	// blob itself.
	SideInfo

	// Broken marks whether the snap is broken and the reason.
	Broken string

	// The information in these fields is ephemeral, available only from the
	// store or when read from a snap file.
	DownloadInfo

	Prices  map[string]float64
	MustBuy bool

	Publisher StoreAccount

	Media   MediaInfos
	Website string

	StoreURL string

	// The flattended channel map with $track/$risk
	Channels map[string]*ChannelSnapInfo

	// The ordered list of tracks that contain channels
	Tracks []string

	Layout map[string]*Layout

	// The list of common-ids from all apps of the snap
	CommonIDs []string

	// List of system users (usernames) this snap may use. The group of the same
	// name must also exist.
	SystemUsernames map[string]*SystemUsernameInfo

	// OriginalLinks is a map links keys to link lists
	OriginalLinks map[string][]string
}

// StoreAccount holds information about a store account, for example of snap
// publisher.
type StoreAccount struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display-name"`
	Validation  string `json:"validation,omitempty"`
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
	ReleasedAt  time.Time       `json:"released-at"`
}

// InstanceName returns the blessed name of the snap decorated with instance
// key, if any.
func (s *Info) InstanceName() string {
	return InstanceName(s.SnapName(), s.InstanceKey)
}

// SnapName returns the global blessed name of the snap.
func (s *Info) SnapName() string {
	if s.RealName != "" {
		return s.RealName
	}
	return s.SuggestedName
}

// Filename returns the name of the snap with the revision number,
// as used on the filesystem. This is the equivalent of
// filepath.Base(s.MountFile()).
func (s *Info) Filename() string {
	return filepath.Base(s.MountFile())
}

// SnapRevision returns the revision of the snap.
func (s *Info) SnapRevision() Revision {
	return s.Revision
}

// ID implements naming.SnapRef.
func (s *Info) ID() string {
	return s.SnapID
}

var _ naming.SnapRef = (*Info)(nil)

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

// Links returns the blessed set of snap-related links.
func (s *Info) Links() map[string][]string {
	if s.EditedLinks != nil {
		return s.EditedLinks
	}
	return s.OriginalLinks
}

// Contact returns the blessed contact information for the snap.
func (s *Info) Contact() string {
	var contact string
	if s.EditedContact != "" {
		contact = s.EditedContact
	} else {
		contacts := s.Links()["contact"]
		if len(contacts) > 0 {
			contact = contacts[0]
		}
	}
	if contact != "" {
		u, err := url.Parse(contact)
		if err != nil {
			return ""
		}
		if u.Scheme == "" {
			contact = "mailto:" + contact
		}
	}
	return contact
}

// Type returns the type of the snap, including additional snap ID check
// for the legacy snapd snap definitions.
func (s *Info) Type() Type {
	if s.SnapType == TypeApp && IsSnapd(s.SnapID) {
		return TypeSnapd
	}
	return s.SnapType
}

// MountDir returns the base directory of the snap where it gets mounted.
func (s *Info) MountDir() string {
	return MountDir(s.InstanceName(), s.Revision)
}

// MountFile returns the path where the snap file that is mounted is installed.
func (s *Info) MountFile() string {
	return MountFile(s.InstanceName(), s.Revision)
}

// HooksDir returns the directory containing the snap's hooks.
func (s *Info) HooksDir() string {
	return HooksDir(s.InstanceName(), s.Revision)
}

// DataDir returns the data directory of the snap.
func (s *Info) DataDir() string {
	return DataDir(s.InstanceName(), s.Revision)
}

// UserDataDir returns the user-specific data directory of the snap.
func (s *Info) UserDataDir(home string, opts *dirs.SnapDirOptions) string {
	return UserDataDir(home, s.InstanceName(), s.Revision, opts)
}

// UserCommonDataDir returns the user-specific data directory common across
// revision of the snap.
func (s *Info) UserCommonDataDir(home string, opts *dirs.SnapDirOptions) string {
	return UserCommonDataDir(home, s.InstanceName(), opts)
}

// CommonDataDir returns the data directory common across revisions of the snap.
func (s *Info) CommonDataDir() string {
	return CommonDataDir(s.InstanceName())
}

func DataHomeGlob(opts *dirs.SnapDirOptions) string {
	if opts == nil {
		opts = &dirs.SnapDirOptions{}
	}

	if opts.HiddenSnapDataDir {
		return dirs.HiddenSnapDataHomeGlob
	}

	return dirs.SnapDataHomeGlob
}

// DataHomeDir returns the per user data directory of the snap.
func (s *Info) DataHomeDir(opts *dirs.SnapDirOptions) string {
	return filepath.Join(DataHomeGlob(opts), s.InstanceName(), s.Revision.String())
}

// CommonDataHomeDir returns the per user data directory common across revisions
// of the snap.
func (s *Info) CommonDataHomeDir(opts *dirs.SnapDirOptions) string {
	return filepath.Join(DataHomeGlob(opts), s.InstanceName(), "common")
}

// UserXdgRuntimeDir returns the XDG_RUNTIME_DIR directory of the snap for a
// particular user.
func (s *Info) UserXdgRuntimeDir(euid sys.UserID) string {
	return UserXdgRuntimeDir(euid, s.InstanceName())
}

// XdgRuntimeDirs returns the XDG_RUNTIME_DIR directories for all users of the
// snap.
func (s *Info) XdgRuntimeDirs() string {
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

// ExpandSnapVariables resolves $SNAP, $SNAP_DATA and $SNAP_COMMON inside the
// snap's mount namespace.
func (s *Info) ExpandSnapVariables(path string) string {
	return os.Expand(path, func(v string) string {
		switch v {
		case "SNAP":
			// NOTE: We use dirs.CoreSnapMountDir here as the path used will be
			// always inside the mount namespace snap-confine creates and there
			// we will always have a /snap directory available regardless if the
			// system we're running on supports this or not.
			return filepath.Join(dirs.CoreSnapMountDir, s.SnapName(), s.Revision.String())
		case "SNAP_DATA":
			return DataDir(s.SnapName(), s.Revision)
		case "SNAP_COMMON":
			return CommonDataDir(s.SnapName())
		}
		return ""
	})
}

// InstallDate returns the "install date" of the snap.
//
// If the snap is not active, it'll return a zero time; otherwise
// it'll return the modtime of the "current" symlink. Sneaky.
func (s *Info) InstallDate() time.Time {
	dir, rev := filepath.Split(s.MountDir())
	cur := filepath.Join(dir, "current")
	tag, err := os.Readlink(cur)
	if err == nil && tag == rev {
		if st, err := os.Lstat(cur); err == nil {
			return st.ModTime()
		}
	}
	return time.Time{}
}

// IsActive returns whether this snap revision is active.
func (s *Info) IsActive() bool {
	dir, rev := filepath.Split(s.MountDir())
	cur := filepath.Join(dir, "current")
	tag, err := os.Readlink(cur)
	return err == nil && tag == rev
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

// DesktopPrefix returns the prefix string for the desktop files that
// belongs to the given snapInstance. We need to do something custom
// here because a) we need to be compatible with the world before we had
// parallel installs b) we can't just use the usual "_" parallel installs
// separator because that is already used as the separator between snap
// and desktop filename.
func (s *Info) DesktopPrefix() string {
	if s.InstanceKey == "" {
		return s.SnapName()
	}
	// we cannot use the usual "_" separator because that is also used
	// to separate "$snap_$desktopfile"
	return fmt.Sprintf("%s+%s", s.SnapName(), s.InstanceKey)
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

func gatherDefaultContentProvider(providerSnapsToContentTag map[string][]string, plug *PlugInfo) {
	if plug.Interface == "content" {
		var dprovider string
		if err := plug.Attr("default-provider", &dprovider); err == nil && dprovider != "" {
			// usage can be "snap:slot" but slot
			// is ignored/unused
			name := strings.Split(dprovider, ":")[0]
			var contentTag string
			plug.Attr("content", &contentTag)
			tags := providerSnapsToContentTag[name]
			if tags == nil {
				tags = []string{contentTag}
			} else {
				if !strutil.SortedListContains(tags, contentTag) {
					tags = append(tags, contentTag)
					sort.Strings(tags)
				}
			}
			providerSnapsToContentTag[name] = tags
		}
	}
}

// DefaultContentProviders returns the set of default provider snaps
// requested by the given plugs, mapped to their content tags.
func DefaultContentProviders(plugs []*PlugInfo) (providerSnapsToContentTag map[string][]string) {
	providerSnapsToContentTag = make(map[string][]string)
	for _, plug := range plugs {
		gatherDefaultContentProvider(providerSnapsToContentTag, plug)
	}
	return providerSnapsToContentTag
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

	// HotplugKey is a unique key built by the slot's interface
	// using properties of a hotplugged device so that the same
	// slot may be made available if the device is reinserted.
	// It's empty for regular slots.
	HotplugKey HotplugKey
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
// (or an empty string if no signal is needed).
func (st StopModeType) KillSignal() string {
	if st.Validate() != nil || st == "" {
		return ""
	}
	return strings.ToUpper(strings.TrimSuffix(string(st), "-all"))
}

// Validate ensures that the StopModeType has an valid value.
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
	CommandChain  []string
	CommonID      string

	Daemon          string
	DaemonScope     DaemonScope
	StopTimeout     timeout.Timeout
	StartTimeout    timeout.Timeout
	WatchdogTimeout timeout.Timeout
	StopCommand     string
	ReloadCommand   string
	PostStopCommand string
	RestartCond     RestartCondition
	RestartDelay    timeout.Timeout
	Completer       string
	RefreshMode     string
	StopMode        StopModeType
	InstallMode     string

	// TODO: this should go away once we have more plumbing and can change
	// things vs refactor
	// https://github.com/snapcore/snapd/pull/794#discussion_r58688496
	BusName     string
	ActivatesOn []*SlotInfo

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
	URL    string `json:"url,omitempty"`
	Width  int64  `json:"width,omitempty"`
	Height int64  `json:"height,omitempty"`
	Note   string `json:"note,omitempty"`
}

type MediaInfo struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Width  int64  `json:"width,omitempty"`
	Height int64  `json:"height,omitempty"`
}

type MediaInfos []MediaInfo

func (mis MediaInfos) IconURL() string {
	for _, mi := range mis {
		if mi.Type == "icon" {
			return mi.URL
		}
	}
	return ""
}

// HookInfo provides information about a hook.
type HookInfo struct {
	Snap *Info

	Name  string
	Plugs map[string]*PlugInfo
	Slots map[string]*SlotInfo

	Environment  strutil.OrderedMap
	CommandChain []string

	Explicit bool
}

// SystemUsernameInfo provides information about a system username (ie, a
// UNIX user and group with the same name). The scope defines visibility of the
// username wrt the snap and the system. Defined scopes:
// - shared    static, snapd-managed user/group shared between host and all
//             snaps
// - private   static, snapd-managed user/group private to a particular snap
//             (currently not implemented)
// - external  dynamic user/group shared between host and all snaps (currently
//             not implented)
type SystemUsernameInfo struct {
	Name  string
	Scope string
	Attrs map[string]interface{}
}

// File returns the path to the *.socket file
func (socket *SocketInfo) File() string {
	return filepath.Join(socket.App.serviceDir(), socket.App.SecurityTag()+"."+socket.Name+".socket")
}

// File returns the path to the *.timer file
func (timer *TimerInfo) File() string {
	return filepath.Join(timer.App.serviceDir(), timer.App.SecurityTag()+".timer")
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

// DesktopFile returns the path to the installed optional desktop file for the
// application.
func (app *AppInfo) DesktopFile() string {
	return filepath.Join(dirs.SnapDesktopFilesDir, fmt.Sprintf("%s_%s.desktop", app.Snap.DesktopPrefix(), app.Name))
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
	if app.Name == app.Snap.SnapName() {
		return fmt.Sprintf("/usr/bin/snap run%s %s", command, app.Snap.InstanceName())
	}
	return fmt.Sprintf("/usr/bin/snap run%s %s.%s", command, app.Snap.InstanceName(), app.Name)
}

// LauncherCommand returns the launcher command line to use when invoking the
// app binary.
func (app *AppInfo) LauncherCommand() string {
	if app.Timer != nil {
		return app.launcherCommand(fmt.Sprintf("--timer=%q", app.Timer.Timer))
	}
	return app.launcherCommand("")
}

// LauncherStopCommand returns the launcher command line to use when invoking
// the app stop command binary.
func (app *AppInfo) LauncherStopCommand() string {
	return app.launcherCommand("--command=stop")
}

// LauncherReloadCommand returns the launcher command line to use when invoking
// the app stop command binary.
func (app *AppInfo) LauncherReloadCommand() string {
	return app.launcherCommand("--command=reload")
}

// LauncherPostStopCommand returns the launcher command line to use when
// invoking the app post-stop command binary.
func (app *AppInfo) LauncherPostStopCommand() string {
	return app.launcherCommand("--command=post-stop")
}

// ServiceName returns the systemd service name for the daemon app.
func (app *AppInfo) ServiceName() string {
	return app.SecurityTag() + ".service"
}

func (app *AppInfo) serviceDir() string {
	switch app.DaemonScope {
	case SystemDaemon:
		return dirs.SnapServicesDir
	case UserDaemon:
		return dirs.SnapUserServicesDir
	default:
		panic("unknown daemon scope")
	}
}

// ServiceFile returns the systemd service file path for the daemon app.
func (app *AppInfo) ServiceFile() string {
	return filepath.Join(app.serviceDir(), app.ServiceName())
}

// IsService returns whether app represents a daemon/service.
func (app *AppInfo) IsService() bool {
	return app.Daemon != ""
}

// EnvChain returns the chain of environment overrides, possibly with
// expandable $ vars, specific for the app.
func (app *AppInfo) EnvChain() []osutil.ExpandableEnv {
	return []osutil.ExpandableEnv{
		{OrderedMap: &app.Snap.Environment},
		{OrderedMap: &app.Environment},
	}
}

// SecurityTag returns the hook-specific security tag.
//
// Security tags are used by various security subsystems as "profile names" and
// sometimes also as a part of the file name.
func (hook *HookInfo) SecurityTag() string {
	return HookSecurityTag(hook.Snap.InstanceName(), hook.Name)
}

// EnvChain returns the chain of environment overrides, possibly with
// expandable $ vars, specific for the hook.
func (hook *HookInfo) EnvChain() []osutil.ExpandableEnv {
	return []osutil.ExpandableEnv{
		{OrderedMap: &hook.Snap.Environment},
		{OrderedMap: &hook.Environment},
	}
}

func infoFromSnapYamlWithSideInfo(meta []byte, si *SideInfo, strk *scopedTracker) (*Info, error) {
	info, err := infoFromSnapYaml(meta, strk)
	if err != nil {
		return nil, err
	}

	if si != nil {
		info.SideInfo = *si
	}

	return info, nil
}

// BrokenSnapError describes an error that refers to a snap that warrants the
// "broken" note.
type BrokenSnapError interface {
	error
	Broken() string
}

type NotFoundError struct {
	Snap     string
	Revision Revision
	// Path encodes the path that triggered the not-found error. It may refer to
	// a file inside the snap or to the snap file itself.
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

// ReadInfo reads the snap information for the installed snap with the given
// name and given side-info.
func ReadInfo(name string, si *SideInfo) (*Info, error) {
	snapYamlFn := filepath.Join(MountDir(name, si.Revision), "meta", "snap.yaml")
	meta, err := ioutil.ReadFile(snapYamlFn)
	if os.IsNotExist(err) {
		return nil, &NotFoundError{Snap: name, Revision: si.Revision, Path: snapYamlFn}
	}
	if err != nil {
		return nil, err
	}

	strk := new(scopedTracker)
	info, err := infoFromSnapYamlWithSideInfo(meta, si, strk)
	if err != nil {
		return nil, &invalidMetaError{Snap: name, Revision: si.Revision, Msg: err.Error()}
	}

	_, instanceKey := SplitInstanceName(name)
	info.InstanceKey = instanceKey

	err = addImplicitHooks(info)
	if err != nil {
		return nil, &invalidMetaError{Snap: name, Revision: si.Revision, Msg: err.Error()}
	}

	bindImplicitHooks(info, strk)

	mountFile := MountFile(name, si.Revision)
	st, err := os.Lstat(mountFile)
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
	// If the file is a regular file than it must be a squashfs file that is
	// used as the backing store for the snap. The size of that file is the
	// size of the snap.
	if st.Mode().IsRegular() {
		info.Size = st.Size()
	}

	return info, nil
}

// ReadCurrentInfo reads the snap information from the installed snap in
// 'current' revision
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

// ReadInfoFromSnapFile reads the snap information from the given Container and
// completes it with the given side-info if this is not nil.
func ReadInfoFromSnapFile(snapf Container, si *SideInfo) (*Info, error) {
	meta, err := snapf.ReadFile("meta/snap.yaml")
	if err != nil {
		return nil, err
	}

	strk := new(scopedTracker)
	info, err := infoFromSnapYamlWithSideInfo(meta, si, strk)
	if err != nil {
		return nil, err
	}

	info.Size, err = snapf.Size()
	if err != nil {
		return nil, err
	}

	addImplicitHooksFromContainer(info, snapf)

	bindImplicitHooks(info, strk)

	err = Validate(info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// InstallDate returns the "install date" of the snap.
//
// If the snap is not active, it'll return a zero time; otherwise it'll return
// the modtime of the "current" symlink.
func InstallDate(name string) time.Time {
	cur := filepath.Join(dirs.SnapMountDir, name, "current")
	if st, err := os.Lstat(cur); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// SplitSnapApp will split a string of the form `snap.app` into the `snap` and
// the `app` part. It also deals with the special case of snapName == appName.
func SplitSnapApp(snapApp string) (snap, app string) {
	l := strings.SplitN(snapApp, ".", 2)
	if len(l) < 2 {
		return l[0], InstanceSnap(l[0])
	}
	return l[0], l[1]
}

// JoinSnapApp produces a full application wrapper name from the `snap` and the
// `app` part. It also deals with the special case of snapName == appName.
func JoinSnapApp(snap, app string) string {
	storeName, instanceKey := SplitInstanceName(snap)
	if storeName == app {
		return InstanceName(app, instanceKey)
	}
	return fmt.Sprintf("%s.%s", snap, app)
}

// InstanceSnap splits the instance name and returns the name of the snap.
func InstanceSnap(instanceName string) string {
	snapName, _ := SplitInstanceName(instanceName)
	return snapName
}

// SplitInstanceName splits the instance name and returns the snap name and the
// instance key.
func SplitInstanceName(instanceName string) (snapName, instanceKey string) {
	split := strings.SplitN(instanceName, "_", 2)
	snapName = split[0]
	if len(split) > 1 {
		instanceKey = split[1]
	}
	return snapName, instanceKey
}

// InstanceName takes the snap name and the instance key and returns an instance
// name of the snap.
func InstanceName(snapName, instanceKey string) string {
	if instanceKey != "" {
		return fmt.Sprintf("%s_%s", snapName, instanceKey)
	}
	return snapName
}

// ByType supports sorting the given slice of snap info by types. The most
// important types will come first.
type ByType []*Info

func (r ByType) Len() int      { return len(r) }
func (r ByType) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r ByType) Less(i, j int) bool {
	return r[i].Type().SortsBefore(r[j].Type())
}

// SortServices sorts the apps based on their Before and After specs, such that
// starting the services in the returned ordering will satisfy all specs.
func SortServices(apps []*AppInfo) (sorted []*AppInfo, err error) {
	nameToApp := make(map[string]*AppInfo, len(apps))
	for _, app := range apps {
		nameToApp[app.Name] = app
	}

	// list of successors of given app
	successors := make(map[string][]*AppInfo, len(apps))
	// count of predecessors (i.e. incoming edges) of given app
	predecessors := make(map[string]int, len(apps))

	// identify the successors and predecessors of each app, input data set may
	// be a subset of all apps in the snap (eg. when restarting only few select
	// apps), thus make sure to look only at those after/before apps that are
	// listed in the input
	for _, app := range apps {
		for _, other := range app.After {
			if _, ok := nameToApp[other]; ok {
				predecessors[app.Name]++
				successors[other] = append(successors[other], app)
			}
		}
		for _, other := range app.Before {
			if _, ok := nameToApp[other]; ok {
				predecessors[other]++
				successors[app.Name] = append(successors[app.Name], nameToApp[other])
			}
		}
	}

	// list of apps without predecessors (no incoming edges)
	queue := make([]*AppInfo, 0, len(apps))
	for _, app := range apps {
		if predecessors[app.Name] == 0 {
			queue = append(queue, app)
		}
	}

	// Kahn:
	// see https://dl.acm.org/citation.cfm?doid=368996.369025
	//     https://en.wikipedia.org/wiki/Topological_sorting%23Kahn%27s_algorithm
	//
	// Apps without predecessors are 'top' nodes. On each iteration, take
	// the next 'top' node, and decrease the predecessor count of each
	// successor app. Once that successor app has no more predecessors, take
	// it out of the predecessors set and add it to the queue of 'top'
	// nodes.
	for len(queue) > 0 {
		app := queue[0]
		queue = queue[1:]
		for _, successor := range successors[app.Name] {
			predecessors[successor.Name]--
			if predecessors[successor.Name] == 0 {
				delete(predecessors, successor.Name)
				queue = append(queue, successor)
			}
		}
		sorted = append(sorted, app)
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
		return nil, fmt.Errorf("applications are part of a before/after cycle: %s", unsatisifed.String())
	}
	return sorted, nil
}

// AppInfoBySnapApp supports sorting the given slice of app infos by
// (instance name, app name).
type AppInfoBySnapApp []*AppInfo

func (a AppInfoBySnapApp) Len() int      { return len(a) }
func (a AppInfoBySnapApp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a AppInfoBySnapApp) Less(i, j int) bool {
	iName := a[i].Snap.InstanceName()
	jName := a[j].Snap.InstanceName()
	if iName == jName {
		return a[i].Name < a[j].Name
	}
	return iName < jName
}
