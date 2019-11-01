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
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var errNoSnap = errors.New("snap not installed")

// snapIcon tries to find the icon inside the snap
func snapIcon(info snap.PlaceInfo) string {
	found, _ := filepath.Glob(filepath.Join(info.MountDir(), "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return ""
	}

	return found[0]
}

func publisherAccount(st *state.State, snapID string) (snap.StoreAccount, error) {
	if snapID == "" {
		return snap.StoreAccount{}, nil
	}

	pubAcct, err := assertstate.Publisher(st, snapID)
	if err != nil {
		return snap.StoreAccount{}, fmt.Errorf("cannot find publisher details: %v", err)
	}
	return snap.StoreAccount{
		ID:          pubAcct.AccountID(),
		Username:    pubAcct.Username(),
		DisplayName: pubAcct.DisplayName(),
		Validation:  pubAcct.Validation(),
	}, nil
}

type aboutSnap struct {
	info   *snap.Info
	snapst *snapstate.SnapState
	health *client.SnapHealth
}

func clientHealthFromHealthstate(h *healthstate.HealthState) *client.SnapHealth {
	if h == nil {
		return nil
	}
	return &client.SnapHealth{
		Revision:  h.Revision,
		Timestamp: h.Timestamp,
		Status:    h.Status.String(),
		Message:   h.Message,
		Code:      h.Code,
	}
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

	info.Publisher, err = publisherAccount(st, info.SnapID)
	if err != nil {
		return aboutSnap{}, err
	}

	health, err := healthstate.Get(st, name)
	if err != nil {
		return aboutSnap{}, err
	}

	return aboutSnap{
		info:   info,
		snapst: &snapst,
		health: clientHealthFromHealthstate(health),
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

	healths, err := healthstate.All(st)
	if err != nil {
		return nil, err
	}

	var firstErr error
	for name, snapst := range snapStates {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		health := clientHealthFromHealthstate(healths[name])
		var aboutThis []aboutSnap
		var info *snap.Info
		var err error
		if all {
			for _, seq := range snapst.Sequence {
				info, err = snap.ReadInfo(name, seq)
				if err != nil {
					// single revision may be broken
					_, instanceKey := snap.SplitInstanceName(name)
					info = &snap.Info{
						SideInfo:    *seq,
						InstanceKey: instanceKey,
						Broken:      err.Error(),
					}
					// clear the error
					err = nil
				}
				info.Publisher, err = publisherAccount(st, seq.SnapID)
				if err != nil && firstErr == nil {
					firstErr = err
				}
				aboutThis = append(aboutThis, aboutSnap{info, snapst, health})
			}
		} else {
			info, err = snapst.CurrentInfo()
			if err == nil {
				info.Publisher, err = publisherAccount(st, info.SnapID)
				aboutThis = append(aboutThis, aboutSnap{info, snapst, health})
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

	sort.Sort(cmd.BySnapApp(appInfos))

	return appInfos, nil
}

func mapLocal(about aboutSnap) *client.Snap {
	localSnap, snapst := about.info, about.snapst
	result, err := cmd.ClientSnapFromSnapInfo(localSnap)
	if err != nil {
		logger.Noticef("cannot get full app info for snap %q: %v", localSnap.InstanceName(), err)
	}
	result.InstalledSize = localSnap.Size

	if icon := snapIcon(localSnap); icon != "" {
		result.Icon = icon
	}

	result.Status = "installed"
	if snapst.Active && localSnap.Revision == snapst.Current {
		result.Status = "active"
	}

	result.TrackingChannel = snapst.TrackingChannel
	result.IgnoreValidation = snapst.IgnoreValidation
	result.CohortKey = snapst.CohortKey
	result.DevMode = snapst.DevMode
	result.TryMode = snapst.TryMode
	result.JailMode = snapst.JailMode
	result.MountedFrom = localSnap.MountFile()
	if result.TryMode {
		// Readlink instead of EvalSymlinks because it's only expected
		// to be one level, and should still resolve if the target does
		// not exist (this might help e.g. snapcraft clean up after a
		// prime dir)
		result.MountedFrom, _ = os.Readlink(result.MountedFrom)
	}
	result.Health = about.health

	return result
}

func mapRemote(remoteSnap *snap.Info) *client.Snap {
	result, err := cmd.ClientSnapFromSnapInfo(remoteSnap)
	if err != nil {
		logger.Noticef("cannot get full app info for snap %q: %v", remoteSnap.SnapName(), err)
	}
	result.DownloadSize = remoteSnap.Size
	if remoteSnap.MustBuy {
		result.Status = "priced"
	} else {
		result.Status = "available"
	}

	return result
}
