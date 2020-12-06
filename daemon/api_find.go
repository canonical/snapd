// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"encoding/json"
	"net"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var (
	findCmd = &Command{
		Path:   "/v2/find",
		UserOK: true,
		GET:    searchStore,
	}
)

func searchStore(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}
	query := r.URL.Query()
	q := query.Get("q")
	commonID := query.Get("common-id")
	// TODO: support both "category" (search v2) and "section"
	section := query.Get("section")
	name := query.Get("name")
	scope := query.Get("scope")
	private := false
	prefix := false

	if sel := query.Get("select"); sel != "" {
		switch sel {
		case "refresh":
			if commonID != "" {
				return BadRequest("cannot use 'common-id' with 'select=refresh'")
			}
			if name != "" {
				return BadRequest("cannot use 'name' with 'select=refresh'")
			}
			if q != "" {
				return BadRequest("cannot use 'q' with 'select=refresh'")
			}
			return storeUpdates(c, r, user)
		case "private":
			private = true
		}
	}

	if name != "" {
		if q != "" {
			return BadRequest("cannot use 'q' and 'name' together")
		}
		if commonID != "" {
			return BadRequest("cannot use 'common-id' and 'name' together")
		}

		if name[len(name)-1] != '*' {
			return findOne(c, r, user, name)
		}

		prefix = true
		q = name[:len(name)-1]
	}

	if commonID != "" && q != "" {
		return BadRequest("cannot use 'common-id' and 'q' together")
	}

	theStore := getStore(c)
	ctx := store.WithClientUserAgent(r.Context(), r)
	found, err := theStore.Find(ctx, &store.Search{
		Query:    q,
		Prefix:   prefix,
		CommonID: commonID,
		Category: section,
		Private:  private,
		Scope:    scope,
	}, user)
	switch err {
	case nil:
		// pass
	case store.ErrBadQuery:
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Result: &errorResult{Message: err.Error(), Kind: client.ErrorKindBadQuery},
			Status: 400,
		}, nil)
	case store.ErrUnauthenticated, store.ErrInvalidCredentials:
		return Unauthorized(err.Error())
	default:
		if e, ok := err.(*url.Error); ok {
			if neterr, ok := e.Err.(*net.OpError); ok {
				if dnserr, ok := neterr.Err.(*net.DNSError); ok {
					return SyncResponse(&resp{
						Type:   ResponseTypeError,
						Result: &errorResult{Message: dnserr.Error(), Kind: client.ErrorKindDNSFailure},
						Status: 400,
					}, nil)
				}
			}
		}
		if e, ok := err.(net.Error); ok && e.Timeout() {
			return SyncResponse(&resp{
				Type:   ResponseTypeError,
				Result: &errorResult{Message: err.Error(), Kind: client.ErrorKindNetworkTimeout},
				Status: 400,
			}, nil)
		}
		if e, ok := err.(*httputil.PersistentNetworkError); ok {
			return SyncResponse(&resp{
				Type:   ResponseTypeError,
				Result: &errorResult{Message: e.Error(), Kind: client.ErrorKindDNSFailure},
				Status: 400,
			}, nil)
		}

		return InternalError("%v", err)
	}

	meta := &Meta{
		SuggestedCurrency: theStore.SuggestedCurrency(),
		Sources:           []string{"store"},
	}

	return sendStorePackages(route, meta, found)
}

func findOne(c *Command, r *http.Request, user *auth.UserState, name string) Response {
	if err := snap.ValidateName(name); err != nil {
		return BadRequest(err.Error())
	}

	theStore := getStore(c)
	spec := store.SnapSpec{
		Name: name,
	}
	ctx := store.WithClientUserAgent(r.Context(), r)
	snapInfo, err := theStore.SnapInfo(ctx, spec, user)
	switch err {
	case nil:
		// pass
	case store.ErrInvalidCredentials:
		return Unauthorized("%v", err)
	case store.ErrSnapNotFound:
		return SnapNotFound(name, err)
	default:
		return InternalError("%v", err)
	}

	meta := &Meta{
		SuggestedCurrency: theStore.SuggestedCurrency(),
		Sources:           []string{"store"},
	}

	results := make([]*json.RawMessage, 1)
	data, err := json.Marshal(webify(mapRemote(snapInfo), r.URL.String()))
	if err != nil {
		return InternalError(err.Error())
	}
	results[0] = (*json.RawMessage)(&data)
	return SyncResponse(results, meta)
}

func storeUpdates(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	state := c.d.overlord.State()
	state.Lock()
	updates, err := snapstateRefreshCandidates(state, user)
	state.Unlock()
	if err != nil {
		return InternalError("cannot list updates: %v", err)
	}

	return sendStorePackages(route, nil, updates)
}

func sendStorePackages(route *mux.Route, meta *Meta, found []*snap.Info) Response {
	results := make([]*json.RawMessage, 0, len(found))
	for _, x := range found {
		url, err := route.URL("name", x.InstanceName())
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", x.InstanceName(), x.Revision, err)
			continue
		}

		data, err := json.Marshal(webify(mapRemote(x), url.String()))
		if err != nil {
			return InternalError("%v", err)
		}
		raw := json.RawMessage(data)
		results = append(results, &raw)
	}

	return SyncResponse(results, meta)
}

func mapRemote(remoteSnap *snap.Info) *client.Snap {
	result, err := clientutil.ClientSnapFromSnapInfo(remoteSnap, nil)
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
