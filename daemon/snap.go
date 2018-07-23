// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

var errNoSnap = errors.New("snap not installed")

// snapIcon tries to find the icon inside the snap
func snapIcon(info *snap.Info) string {
	// XXX: copy of snap.Snap.Icon which will go away
	found, _ := filepath.Glob(filepath.Join(info.MountDir(), "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return info.IconURL
	}

	return found[0]
}

func publisherAccount(st *state.State, info *snap.Info) (*snap.StoreAccount, error) {
	if info.SnapID == "" {
		return nil, nil
	}

	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	if err != nil {
		return nil, fmt.Errorf("cannot find publisher details: %v", err)
	}
	return &snap.StoreAccount{
		ID:          pubAcct.AccountID(),
		Username:    pubAcct.Username(),
		DisplayName: pubAcct.DisplayName(),
		Validation:  pubAcct.Validation(),
	}, nil
}

type aboutSnap struct {
	info      *snap.Info
	snapst    *snapstate.SnapState
	publisher *snap.StoreAccount
}

// localSnapInfo returns the information about the current snap for the given name plus the SnapState with the active flag and other snap revisions.
func localSnapInfo(st *state.State, name string) (aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return aboutSnap{}, fmt.Errorf("cannot consult state: %v", err)
	}

	info, err := snapst.CurrentInfo()
	if err == snapstate.ErrNoCurrent {
		return aboutSnap{}, errNoSnap
	}
	if err != nil {
		return aboutSnap{}, fmt.Errorf("cannot read snap details: %v", err)
	}

	publisher, err := publisherAccount(st, info)
	if err != nil {
		return aboutSnap{}, err
	}

	return aboutSnap{
		info:      info,
		snapst:    &snapst,
		publisher: publisher,
	}, nil
}

// allLocalSnapInfos returns the information about the all current snaps and their SnapStates.
func allLocalSnapInfos(st *state.State, all bool, wanted map[string]bool) ([]aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	about := make([]aboutSnap, 0, len(snapStates))

	var firstErr error
	for name, snapst := range snapStates {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		var aboutThis []aboutSnap
		var info *snap.Info
		var publisher *snap.StoreAccount
		var err error
		if all {
			for _, seq := range snapst.Sequence {
				info, err = snap.ReadInfo(seq.RealName, seq)
				if err != nil {
					break
				}
				publisher, err = publisherAccount(st, info)
				aboutThis = append(aboutThis, aboutSnap{info, snapst, publisher})
			}
		} else {
			info, err = snapst.CurrentInfo()
			if err == nil {
				var publisher *snap.StoreAccount
				publisher, err = publisherAccount(st, info)
				aboutThis = append(aboutThis, aboutSnap{info, snapst, publisher})
			}
		}

		if err != nil {
			// XXX: aggregate instead?
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		about = append(about, aboutThis...)
	}

	return about, firstErr
}

type bySnapApp []*snap.AppInfo

func (a bySnapApp) Len() int      { return len(a) }
func (a bySnapApp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a bySnapApp) Less(i, j int) bool {
	iName := a[i].Snap.InstanceName()
	jName := a[j].Snap.InstanceName()
	if iName == jName {
		return a[i].Name < a[j].Name
	}
	return iName < jName
}

// this differs from snap.SplitSnapApp in the handling of the
// snap-only case:
//   snap.SplitSnapApp("foo") is ("foo", "foo"),
//   splitAppName("foo") is ("foo", "").
func splitAppName(s string) (snap, app string) {
	if idx := strings.IndexByte(s, '.'); idx > -1 {
		return s[:idx], s[idx+1:]
	}

	return s, ""
}

type appInfoOptions struct {
	service bool
}

func (opts appInfoOptions) String() string {
	if opts.service {
		return "service"
	}

	return "app"
}

// appInfosFor returns a sorted list apps described by names.
//
// * If names is empty, returns all apps of the wanted kinds (which
//   could be an empty list).
// * An element of names can be a snap name, in which case all apps
//   from the snap of the wanted kind are included in the result (and
//   it's an error if the snap has no apps of the wanted kind).
// * An element of names can instead be snap.app, in which case that app is
//   included in the result (and it's an error if the snap and app don't
//   both exist, or if the app is not a wanted kind)
// On error an appropriate error Response is returned; a nil Response means
// no error.
//
// It's a programming error to call this with wanted having neither
// services nor commands set.
func appInfosFor(st *state.State, names []string, opts appInfoOptions) ([]*snap.AppInfo, Response) {
	snapNames := make(map[string]bool)
	requested := make(map[string]bool)
	for _, name := range names {
		requested[name] = true
		name, _ = splitAppName(name)
		snapNames[name] = true
	}

	snaps, err := allLocalSnapInfos(st, false, snapNames)
	if err != nil {
		return nil, InternalError("cannot list local snaps! %v", err)
	}

	found := make(map[string]bool)
	appInfos := make([]*snap.AppInfo, 0, len(requested))
	for _, snp := range snaps {
		snapName := snp.info.InstanceName()
		apps := make([]*snap.AppInfo, 0, len(snp.info.Apps))
		for _, app := range snp.info.Apps {
			if !opts.service || app.IsService() {
				apps = append(apps, app)
			}
		}

		if len(apps) == 0 && requested[snapName] {
			return nil, AppNotFound("snap %q has no %ss", snapName, opts)
		}

		includeAll := len(requested) == 0 || requested[snapName]
		if includeAll {
			// want all services in a snap
			found[snapName] = true
		}

		for _, app := range apps {
			appName := snapName + "." + app.Name
			if includeAll || requested[appName] {
				appInfos = append(appInfos, app)
				found[appName] = true
			}
		}
	}

	for k := range requested {
		if !found[k] {
			if snapNames[k] {
				return nil, SnapNotFound(k, fmt.Errorf("snap %q not found", k))
			} else {
				snap, app := splitAppName(k)
				return nil, AppNotFound("snap %q has no %s %q", snap, opts, app)
			}
		}
	}

	sort.Sort(bySnapApp(appInfos))

	return appInfos, nil
}

func clientAppInfosFromSnapAppInfos(apps []*snap.AppInfo) []client.AppInfo {
	// TODO: pass in an actual notifier here instead of null
	//       (Status doesn't _need_ it, but benefits from it)
	sysd := systemd.New(dirs.GlobalRootDir, progress.Null)

	out := make([]client.AppInfo, len(apps))
	for i, app := range apps {
		out[i] = client.AppInfo{
			Snap:     app.Snap.InstanceName(),
			Name:     app.Name,
			CommonID: app.CommonID,
		}
		if fn := app.DesktopFile(); osutil.FileExists(fn) {
			out[i].DesktopFile = fn
		}

		if app.IsService() {
			// TODO: look into making a single call to Status for all services
			if sts, err := sysd.Status(app.ServiceName()); err != nil {
				logger.Noticef("cannot get status of service %q: %v", app.Name, err)
			} else if len(sts) != 1 {
				logger.Noticef("cannot get status of service %q: expected 1 result, got %d", app.Name, len(sts))
			} else {
				out[i].Daemon = sts[0].Daemon
				out[i].Enabled = sts[0].Enabled
				out[i].Active = sts[0].Active
			}
		}
	}

	return out
}

func mapLocal(about aboutSnap) *client.Snap {
	localSnap, snapst := about.info, about.snapst
	status := "installed"
	if snapst.Active && localSnap.Revision == snapst.Current {
		status = "active"
	}

	snapapps := make([]*snap.AppInfo, 0, len(localSnap.Apps))
	for _, app := range localSnap.Apps {
		snapapps = append(snapapps, app)
	}
	sort.Sort(bySnapApp(snapapps))

	apps := clientAppInfosFromSnapAppInfos(snapapps)

	// TODO: expose aliases information and state?

	publisherUsername := ""
	if about.publisher != nil {
		publisherUsername = about.publisher.Username
	}
	result := &client.Snap{
		Description:      localSnap.Description(),
		Developer:        publisherUsername,
		Publisher:        about.publisher,
		Icon:             snapIcon(localSnap),
		ID:               localSnap.SnapID,
		InstallDate:      localSnap.InstallDate(),
		InstalledSize:    localSnap.Size,
		Name:             localSnap.InstanceName(),
		Revision:         localSnap.Revision,
		Status:           status,
		Summary:          localSnap.Summary(),
		Type:             string(localSnap.Type),
		Base:             localSnap.Base,
		Version:          localSnap.Version,
		Channel:          localSnap.Channel,
		TrackingChannel:  snapst.Channel,
		IgnoreValidation: snapst.IgnoreValidation,
		Confinement:      string(localSnap.Confinement),
		DevMode:          snapst.DevMode,
		TryMode:          snapst.TryMode,
		JailMode:         snapst.JailMode,
		Private:          localSnap.Private,
		Apps:             apps,
		Broken:           localSnap.Broken,
		Contact:          localSnap.Contact,
		Title:            localSnap.Title(),
		License:          localSnap.License,
		CommonIDs:        localSnap.CommonIDs,
		MountedFrom:      localSnap.MountFile(),
	}

	if result.TryMode {
		// Readlink instead of EvalSymlinks because it's only expected
		// to be one level, and should still resolve if the target does
		// not exist (this might help e.g. snapcraft clean up after a
		// prime dir)
		result.MountedFrom, _ = os.Readlink(result.MountedFrom)
	}

	return result
}

func mapRemote(remoteSnap *snap.Info) *client.Snap {
	status := "available"
	if remoteSnap.MustBuy {
		status = "priced"
	}

	confinement := remoteSnap.Confinement
	if confinement == "" {
		confinement = snap.StrictConfinement
	}

	screenshots := make([]client.Screenshot, len(remoteSnap.Screenshots))
	for i, screenshot := range remoteSnap.Screenshots {
		screenshots[i] = client.Screenshot{
			URL:    screenshot.URL,
			Width:  screenshot.Width,
			Height: screenshot.Height,
		}
	}

	publisher := remoteSnap.Publisher
	result := &client.Snap{
		Description:  remoteSnap.Description(),
		Developer:    remoteSnap.Publisher.Username,
		Publisher:    &publisher,
		DownloadSize: remoteSnap.Size,
		Icon:         snapIcon(remoteSnap),
		ID:           remoteSnap.SnapID,
		Name:         remoteSnap.InstanceName(),
		Revision:     remoteSnap.Revision,
		Status:       status,
		Summary:      remoteSnap.Summary(),
		Type:         string(remoteSnap.Type),
		Base:         remoteSnap.Base,
		Version:      remoteSnap.Version,
		Channel:      remoteSnap.Channel,
		Private:      remoteSnap.Private,
		Confinement:  string(confinement),
		Contact:      remoteSnap.Contact,
		Title:        remoteSnap.Title(),
		License:      remoteSnap.License,
		Screenshots:  screenshots,
		Prices:       remoteSnap.Prices,
		Channels:     remoteSnap.Channels,
		Tracks:       remoteSnap.Tracks,
		CommonIDs:    remoteSnap.CommonIDs,
	}

	return result
}
