// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"crypto"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snap"
)

// TODO: rename this type to something more general, since it is used for more
// than just refreshes
type RefreshOptions struct {
	// RefreshManaged indicates to the store that the refresh is
	// managed via snapd-control.
	RefreshManaged bool
	Scheduled      bool

	PrivacyKey string

	// IncludeResources indicates to the store that resources should be included
	// in the response.
	IncludeResources bool
}

// snap action: install/refresh

type CurrentSnap struct {
	InstanceName     string
	SnapID           string
	Revision         snap.Revision
	TrackingChannel  string
	RefreshedDate    time.Time
	IgnoreValidation bool
	Block            []snap.Revision
	Epoch            snap.Epoch
	CohortKey        string
	// ValidationSets is an optional array of validation set primary keys.
	ValidationSets []snapasserts.ValidationSetKey
	// HeldBy is an optional array of snaps with holds on the current snap's
	// refreshes. The "system" snap represents a hold placed by the user.
	HeldBy []string
}

type AssertionQuery interface {
	ToResolve() (map[asserts.Grouping][]*asserts.AtRevision, map[asserts.Grouping][]*asserts.AtSequence, error)

	AddError(e error, ref *asserts.Ref) error
	AddSequenceError(e error, atSeq *asserts.AtSequence) error
	AddGroupingError(e error, grouping asserts.Grouping) error
}

type currentSnapV2JSON struct {
	SnapID           string     `json:"snap-id"`
	InstanceKey      string     `json:"instance-key"`
	Revision         int        `json:"revision"`
	TrackingChannel  string     `json:"tracking-channel"`
	Epoch            snap.Epoch `json:"epoch"`
	RefreshedDate    *time.Time `json:"refreshed-date,omitempty"`
	IgnoreValidation bool       `json:"ignore-validation,omitempty"`
	CohortKey        string     `json:"cohort-key,omitempty"`
	// ValidationSets is an optional array of validation set primary keys.
	ValidationSets [][]string `json:"validation-sets,omitempty"`
	// Held is an optional map that can contain a "by" key mapping to a list of
	// snaps with holds on the current snap (see CurrentSnap#Held).
	Held map[string][]string `json:"held,omitempty"`
}

type SnapActionFlags int

const (
	SnapActionIgnoreValidation SnapActionFlags = 1 << iota
	SnapActionEnforceValidation
)

type SnapAction struct {
	Action       string
	InstanceName string
	SnapID       string
	Channel      string
	Revision     snap.Revision
	CohortKey    string
	Flags        SnapActionFlags
	Epoch        snap.Epoch
	// ValidationSets is an optional array of validation set primary keys
	// (relevant for install and refresh actions).
	ValidationSets []snapasserts.ValidationSetKey
}

func isValidAction(action string) bool {
	switch action {
	case "download", "install", "refresh":
		return true
	default:
		return false
	}
}

type snapActionJSON struct {
	Action string `json:"action"`
	// For snap
	InstanceKey      string `json:"instance-key,omitempty"`
	Name             string `json:"name,omitempty"`
	SnapID           string `json:"snap-id,omitempty"`
	Channel          string `json:"channel,omitempty"`
	Revision         int    `json:"revision,omitempty"`
	CohortKey        string `json:"cohort-key,omitempty"`
	IgnoreValidation *bool  `json:"ignore-validation,omitempty"`

	// NOTE the store needs an epoch (even if null) for the "install" and "download"
	// actions, to know the client handles epochs at all.  "refresh" actions should
	// send nothing, not even null -- the snap in the context should have the epoch
	// already.  We achieve this by making Epoch be an `interface{}` with omitempty,
	// and then setting it to a (possibly nil) epoch for install and download. As a
	// nil epoch is not an empty interface{}, you'll get the null in the json.
	Epoch interface{} `json:"epoch,omitempty"`
	// For assertions
	Key            string        `json:"key,omitempty"`
	Assertions     []interface{} `json:"assertions,omitempty"`
	ValidationSets [][]string    `json:"validation-sets,omitempty"`
}

type assertAtJSON struct {
	Type        string   `json:"type"`
	PrimaryKey  []string `json:"primary-key"`
	IfNewerThan *int     `json:"if-newer-than,omitempty"`
}

type assertSeqAtJSON struct {
	Type        string   `json:"type"`
	SequenceKey []string `json:"sequence-key"`
	Sequence    int      `json:"sequence,omitempty"`
	// if-sequence-equal-or-newer-than and sequence are mutually exclusive
	IfSequenceEqualOrNewerThan *int `json:"if-sequence-equal-or-newer-than,omitempty"`
	IfSequenceNewerThan        *int `json:"if-sequence-newer-than,omitempty"`
	IfNewerThan                *int `json:"if-newer-than,omitempty"`
}

type snapRelease struct {
	Architecture string `json:"architecture"`
	Channel      string `json:"channel"`
}

type errorListEntry struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// for assertions
	Type string `json:"type"`
	// either primary-key or sequence-key is expected (but not both)
	PrimaryKey  []string `json:"primary-key,omitempty"`
	SequenceKey []string `json:"sequence-key,omitempty"`
}

type snapActionResult struct {
	Result string `json:"result"`
	// For snap
	InstanceKey      string    `json:"instance-key"`
	SnapID           string    `json:"snap-id,omitempty"`
	Name             string    `json:"name,omitempty"`
	Snap             storeSnap `json:"snap"`
	EffectiveChannel string    `json:"effective-channel,omitempty"`
	RedirectChannel  string    `json:"redirect-channel,omitempty"`
	Error            struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Extra   struct {
			Releases []snapRelease `json:"releases"`
		} `json:"extra"`
	} `json:"error"`
	// For assertions
	Key                 string           `json:"key"`
	AssertionStreamURLs []string         `json:"assertion-stream-urls"`
	ErrorList           []errorListEntry `json:"error-list"`
}

type snapActionRequest struct {
	Context             []*currentSnapV2JSON `json:"context"`
	Actions             []*snapActionJSON    `json:"actions"`
	Fields              []string             `json:"fields"`
	AssertionMaxFormats map[string]int       `json:"assertion-max-formats,omitempty"`
}

type snapActionResultList struct {
	Results   []*snapActionResult `json:"results"`
	ErrorList []errorListEntry    `json:"error-list"`
}

var snapActionFields = jsonutil.StructFields((*storeSnap)(nil), "resources")

// SnapAction queries the store for snap information for the given
// install/refresh actions, given the context information about
// current installed snaps in currentSnaps. If the request was overall
// successful (200) but there were reported errors it will return both
// the snap infos and an SnapActionError.
// Orthogonally and at the same time it can be used to fetch or update
// assertions by passing an AssertionQuery whose ToResolve specifies
// the assertions and revisions to consider. Assertion related errors
// are reported via the AssertionQuery Add*Error methods.
func (s *Store) SnapAction(ctx context.Context, currentSnaps []*CurrentSnap, actions []*SnapAction, assertQuery AssertionQuery, user *auth.UserState, opts *RefreshOptions) ([]SnapActionResult, []AssertionResult, error) {
	if opts == nil {
		opts = &RefreshOptions{}
	}

	var toResolve map[asserts.Grouping][]*asserts.AtRevision
	var toResolveSeq map[asserts.Grouping][]*asserts.AtSequence
	if assertQuery != nil {
		var err error
		toResolve, toResolveSeq, err = assertQuery.ToResolve()
		if err != nil {
			return nil, nil, err
		}
	}

	if len(currentSnaps) == 0 && len(actions) == 0 && len(toResolve) == 0 && len(toResolveSeq) == 0 {
		// nothing to do
		return nil, nil, &SnapActionError{NoResults: true}
	}

	authRefreshes := 0
	for {
		sars, ars, err := s.snapAction(ctx, currentSnaps, actions, assertQuery, toResolve, toResolveSeq, user, opts, 0)

		if saErr, ok := err.(*SnapActionError); ok && authRefreshes < 2 && len(saErr.Other) > 0 {
			// do we need to try to refresh auths?, 2 tries
			var refreshNeed AuthRefreshNeed
			for _, otherErr := range saErr.Other {
				switch otherErr {
				case errUserAuthorizationNeedsRefresh:
					refreshNeed.User = true
				case errDeviceAuthorizationNeedsRefresh:
					refreshNeed.Device = true
				}
			}
			if refreshNeed.needed() {
				if a, ok := s.auth.(RefreshingAuthorizer); ok {
					err := a.RefreshAuth(refreshNeed, s.dauthCtx, user, s.client)
					if err != nil {
						// best effort
						logger.Noticef("cannot refresh soft-expired authorisation: %v", err)
					}
					authRefreshes++
					// TODO: we could avoid retrying here
					// if refreshAuth gave no error we got
					// as many non-error results from the
					// store as actions anyway
					continue
				}
			}
		}

		return sars, ars, err
	}
}

func genInstanceKey(curSnap *CurrentSnap, salt string) (string, error) {
	_, snapInstanceKey := snap.SplitInstanceName(curSnap.InstanceName)

	if snapInstanceKey == "" {
		return curSnap.SnapID, nil
	}

	if salt == "" {
		return "", fmt.Errorf("internal error: request salt not provided")
	}

	// due to privacy concerns, avoid sending the local names to the
	// backend, instead hash the snap ID and instance key together
	h := crypto.SHA256.New()
	h.Write([]byte(curSnap.SnapID))
	h.Write([]byte(snapInstanceKey))
	h.Write([]byte(salt))
	enc := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%s:%s", curSnap.SnapID, enc), nil
}

// SnapActionResult encapsulates the non-error result of a single
// action of the SnapAction call.
type SnapActionResult struct {
	*snap.Info
	Resources       []SnapResourceResult
	RedirectChannel string
}

type SnapResourceResult struct {
	DownloadInfo snap.DownloadInfo
	Type         string
	Name         string
	Revision     int
	Version      string
	CreatedAt    string
}

func (sar *SnapActionResult) ResourceResult(resName string) *SnapResourceResult {
	for _, res := range sar.Resources {
		if res.Name == resName {
			return &res
		}
	}
	return nil
}

// ResourceToComponentType returns a validated component type from a resource type.
func ResourceToComponentType(resType string) (snap.ComponentType, error) {
	compTp := strings.TrimPrefix(resType, "component/")
	if len(compTp) == len(resType) {
		return "", fmt.Errorf("%s is not a component resource", resType)
	}
	return snap.ComponentTypeFromString(compTp)
}

// AssertionResult encapsulates the non-error result for one assertion
// grouping fetch action.
type AssertionResult struct {
	Grouping   asserts.Grouping
	StreamURLs []string
}

func (s *Store) snapAction(ctx context.Context, currentSnaps []*CurrentSnap, actions []*SnapAction, assertQuery AssertionQuery, toResolve map[asserts.Grouping][]*asserts.AtRevision, toResolveSeq map[asserts.Grouping][]*asserts.AtSequence, user *auth.UserState, opts *RefreshOptions, storeVer int) ([]SnapActionResult, []AssertionResult, error) {
	requestSalt := ""
	if opts != nil {
		requestSalt = opts.PrivacyKey
	}
	curSnaps := make(map[string]*CurrentSnap, len(currentSnaps))
	curSnapJSONs := make([]*currentSnapV2JSON, len(currentSnaps))
	instanceNameToKey := make(map[string]string, len(currentSnaps))
	for i, curSnap := range currentSnaps {
		if curSnap.SnapID == "" || curSnap.InstanceName == "" || curSnap.Revision.Unset() {
			return nil, nil, fmt.Errorf("internal error: invalid current snap information")
		}
		instanceKey, err := genInstanceKey(curSnap, requestSalt)
		if err != nil {
			return nil, nil, err
		}
		curSnaps[instanceKey] = curSnap
		instanceNameToKey[curSnap.InstanceName] = instanceKey

		channel := curSnap.TrackingChannel
		if channel == "" {
			channel = "stable"
		}
		var refreshedDate *time.Time
		if !curSnap.RefreshedDate.IsZero() {
			refreshedDate = &curSnap.RefreshedDate
		}

		valsetKeys := make([][]string, 0, len(curSnap.ValidationSets))
		for _, vsKey := range curSnap.ValidationSets {
			valsetKeys = append(valsetKeys, vsKey.Components())
		}

		curSnapJSONs[i] = &currentSnapV2JSON{
			SnapID:           curSnap.SnapID,
			InstanceKey:      instanceKey,
			Revision:         curSnap.Revision.N,
			TrackingChannel:  channel,
			IgnoreValidation: curSnap.IgnoreValidation,
			RefreshedDate:    refreshedDate,
			Epoch:            curSnap.Epoch,
			CohortKey:        curSnap.CohortKey,
			ValidationSets:   valsetKeys,
		}
		// `held` field was introduced in version 55 https://api.snapcraft.io/docs/
		if len(curSnap.HeldBy) > 0 && (storeVer <= 0 || storeVer >= 55) {
			curSnapJSONs[i].Held = map[string][]string{"by": curSnap.HeldBy}
		}
	}

	// do not include toResolveSeq len in the initial size since it may have
	// group keys overlapping with toResolve; the loop over toResolveSeq simply
	// appends to actionJSONs.
	actionJSONs := make([]*snapActionJSON, len(actions)+len(toResolve))
	actionIndex := 0

	// snaps
	downloadNum := 0
	installNum := 0
	installs := make(map[string]*SnapAction, len(actions))
	downloads := make(map[string]*SnapAction, len(actions))
	refreshes := make(map[string]*SnapAction, len(actions))
	for _, a := range actions {
		if !isValidAction(a.Action) {
			return nil, nil, fmt.Errorf("internal error: unsupported action %q", a.Action)
		}
		if a.InstanceName == "" {
			return nil, nil, fmt.Errorf("internal error: action without instance name")
		}
		var ignoreValidation *bool
		if a.Flags&SnapActionIgnoreValidation != 0 {
			var t = true
			ignoreValidation = &t
		} else if a.Flags&SnapActionEnforceValidation != 0 {
			var f = false
			ignoreValidation = &f
		}

		valsetKeyComponents := make([][]string, 0, len(a.ValidationSets))
		for _, vsKey := range a.ValidationSets {
			valsetKeyComponents = append(valsetKeyComponents, vsKey.Components())
		}

		var instanceKey string
		aJSON := &snapActionJSON{
			Action:           a.Action,
			SnapID:           a.SnapID,
			Channel:          a.Channel,
			Revision:         a.Revision.N,
			CohortKey:        a.CohortKey,
			ValidationSets:   valsetKeyComponents,
			IgnoreValidation: ignoreValidation,
		}

		if a.Action == "install" {
			installNum++
			instanceKey = fmt.Sprintf("install-%d", installNum)
			installs[instanceKey] = a
		} else if a.Action == "download" {
			downloadNum++
			instanceKey = fmt.Sprintf("download-%d", downloadNum)
			downloads[instanceKey] = a
			if _, key := snap.SplitInstanceName(a.InstanceName); key != "" {
				return nil, nil, fmt.Errorf("internal error: unsupported download with instance name %q", a.InstanceName)
			}
		} else {
			instanceKey = instanceNameToKey[a.InstanceName]
			refreshes[instanceKey] = a
		}

		if a.Action != "refresh" {
			aJSON.Name = snap.InstanceSnap(a.InstanceName)
			if a.Epoch.IsZero() {
				// Let the store know we can handle epochs, by sending the `epoch`
				// field in the request.  A nil epoch is not an empty interface{},
				// you'll get the null in the json. See comment in snapActionJSON.
				aJSON.Epoch = (*snap.Epoch)(nil)
			} else {
				// this is the amend case
				aJSON.Epoch = &a.Epoch
			}
		}

		aJSON.InstanceKey = instanceKey

		actionJSONs[actionIndex] = aJSON
		actionIndex++
	}

	groupingsAssertions := make(map[string]*snapActionJSON)

	// assertions
	var assertMaxFormats map[string]int
	if len(toResolve) > 0 {
		for grp, ats := range toResolve {
			aJSON := &snapActionJSON{
				Action: "fetch-assertions",
				Key:    string(grp),
			}
			aJSON.Assertions = make([]interface{}, len(ats))
			groupingsAssertions[aJSON.Key] = aJSON

			for j, at := range ats {
				aj := &assertAtJSON{
					Type:       at.Type.Name,
					PrimaryKey: asserts.ReducePrimaryKey(at.Type, at.PrimaryKey),
				}
				rev := at.Revision
				if rev != asserts.RevisionNotKnown {
					aj.IfNewerThan = &rev
				}
				aJSON.Assertions[j] = aj
			}
			actionJSONs[actionIndex] = aJSON
			actionIndex++
		}
	}

	if len(toResolveSeq) > 0 {
		for grp, ats := range toResolveSeq {
			key := string(grp)
			// append to existing grouping if applicable
			aJSON := groupingsAssertions[key]
			existingGroup := aJSON != nil
			if !existingGroup {
				aJSON = &snapActionJSON{
					Action: "fetch-assertions",
					Key:    key,
				}
				aJSON.Assertions = make([]interface{}, 0, len(ats))
				actionJSONs = append(actionJSONs, aJSON)
			}
			for _, at := range ats {
				aj := assertSeqAtJSON{
					Type:        at.Type.Name,
					SequenceKey: at.SequenceKey,
				}
				// for pinned we request the assertion ​by the sequence point <sequence-number>​, i.e.
				// {"type": "validation-set",
				//  "sequence-key": ["16", "account-id", "name"],
				//  "sequence": <sequence-number>}
				if at.Pinned {
					if at.Sequence <= 0 {
						return nil, nil, fmt.Errorf("internal error: sequence not set for pinned sequence %s, %v", at.Type.Name, at.SequenceKey)
					}
					aj.Sequence = at.Sequence
				} else {
					// for not pinned, if sequence is specified, then
					// use it for "if-sequence-equal-or-newer-than": <sequence-number>
					if at.Sequence > 0 {
						aj.IfSequenceEqualOrNewerThan = &at.Sequence
					} // else - get the latest
				}
				rev := at.Revision
				// revision (if set) goes to "if-newer-than": <assert-revision>
				if rev != asserts.RevisionNotKnown {
					if at.Sequence <= 0 {
						return nil, nil, fmt.Errorf("internal error: sequence not set while revision is known for %s, %v", at.Type.Name, at.SequenceKey)
					}
					aj.IfNewerThan = &rev
				}
				aJSON.Assertions = append(aJSON.Assertions, aj)
			}
		}
	}

	if len(toResolve) > 0 || len(toResolveSeq) > 0 {
		if s.cfg.AssertionMaxFormats == nil {
			assertMaxFormats = asserts.MaxSupportedFormats(1)
		} else {
			assertMaxFormats = s.cfg.AssertionMaxFormats
		}
	}

	fields := make([]string, len(snapActionFields))
	copy(fields, snapActionFields)
	if opts.IncludeResources {
		fields = append(fields, "resources")
	}

	// build input for the install/refresh endpoint
	jsonData, err := json.Marshal(snapActionRequest{
		Context:             curSnapJSONs,
		Actions:             actionJSONs,
		Fields:              fields,
		AssertionMaxFormats: assertMaxFormats,
	})
	if err != nil {
		return nil, nil, err
	}

	u, err := s.endpointURL(snapActionEndpPath, nil)
	if err != nil {
		return nil, nil, err
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         u,
		Accept:      jsonContentType,
		ContentType: jsonContentType,
		Data:        jsonData,
		APILevel:    apiV2Endps,
	}

	if opts.Scheduled {
		logger.Debugf("Auto-refresh; adding header Snap-Refresh-Reason: scheduled")
		reqOptions.addHeader("Snap-Refresh-Reason", "scheduled")
	}

	if s.useDeltas() {
		logger.Debugf("Deltas enabled. Adding header Snap-Accept-Delta-Format: %v", s.deltaFormat)
		reqOptions.addHeader("Snap-Accept-Delta-Format", s.deltaFormat)
	}
	if opts.RefreshManaged {
		reqOptions.addHeader("Snap-Refresh-Managed", "true")
	}

	var results snapActionResultList
	resp, err := s.retryRequestDecodeJSON(ctx, reqOptions, user, &results, nil)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != 200 {
		// some fields might not be supported on proxies with old versions.
		// we should retry with the snap store version known as we can now
		// get it from the response header.
		if resp.StatusCode == 400 && storeVer <= 0 {
			verstr := resp.Header.Get("Snap-Store-Version")
			ver, err := strconv.Atoi(verstr)
			if err != nil || ver <= 0 {
				logger.Debugf("cannot parse header value of Snap-Store-Version: expected positive int got %q", verstr)
			} else {
				return s.snapAction(ctx, currentSnaps, actions, assertQuery, toResolve, toResolveSeq, user, opts, ver)
			}
		}
		return nil, nil, respToError(resp, "query the store for updates")
	}

	s.extractSuggestedCurrency(resp)

	refreshErrors := make(map[string]error)
	installErrors := make(map[string]error)
	downloadErrors := make(map[string]error)
	var otherErrors []error

	var sars []SnapActionResult
	var ars []AssertionResult
	for _, res := range results.Results {
		if res.Result == "fetch-assertions" {
			if len(res.ErrorList) != 0 {
				if err := reportFetchAssertionsError(res, assertQuery); err != nil {
					return nil, nil, fmt.Errorf("internal error: %v", err)
				}
				continue
			}
			ars = append(ars, AssertionResult{
				Grouping:   asserts.Grouping(res.Key),
				StreamURLs: res.AssertionStreamURLs,
			})
			continue
		}
		if res.Result == "error" {
			if a := installs[res.InstanceKey]; a != nil {
				if res.Name != "" {
					installErrors[a.InstanceName] = translateSnapActionError("install", a.Channel, res.Error.Code, res.Error.Message, res.Error.Extra.Releases)
					continue
				}
			} else if a := downloads[res.InstanceKey]; a != nil {
				if res.Name != "" {
					downloadErrors[res.Name] = translateSnapActionError("download", a.Channel, res.Error.Code, res.Error.Message, res.Error.Extra.Releases)
					continue
				}
			} else {
				if cur := curSnaps[res.InstanceKey]; cur != nil {
					a := refreshes[res.InstanceKey]
					if a == nil {
						// got an error for a snap that was not part of an 'action'
						otherErrors = append(otherErrors, translateSnapActionError("", "", res.Error.Code, fmt.Sprintf("snap %q: %s", cur.InstanceName, res.Error.Message), nil))
						logger.Debugf("Unexpected error for snap %q, instance key %v: [%v] %v", cur.InstanceName, res.InstanceKey, res.Error.Code, res.Error.Message)
						continue
					}
					channel := a.Channel
					if channel == "" && a.Revision.Unset() {
						channel = cur.TrackingChannel
					}
					refreshErrors[cur.InstanceName] = translateSnapActionError("refresh", channel, res.Error.Code, res.Error.Message, res.Error.Extra.Releases)
					continue
				}
			}
			otherErrors = append(otherErrors, translateSnapActionError("", "", res.Error.Code, res.Error.Message, nil))
			continue
		}
		snapInfo, err := infoFromStoreSnap(&res.Snap)
		if err != nil {
			return nil, nil, fmt.Errorf("unexpected invalid install/refresh API result: %v", err)
		}

		snapInfo.Channel = res.EffectiveChannel

		var instanceName string
		if res.Result == "refresh" {
			cur := curSnaps[res.InstanceKey]
			if cur == nil {
				return nil, nil, fmt.Errorf("unexpected invalid install/refresh API result: unexpected refresh")
			}
			rrev := snap.R(res.Snap.Revision)
			if rrev == cur.Revision || findRev(rrev, cur.Block) {
				refreshErrors[cur.InstanceName] = ErrNoUpdateAvailable
				continue
			}
			instanceName = cur.InstanceName
		} else if res.Result == "install" {
			if action := installs[res.InstanceKey]; action != nil {
				instanceName = action.InstanceName
			}
		}

		if res.Result != "download" && instanceName == "" {
			return nil, nil, fmt.Errorf("unexpected invalid install/refresh API result: unexpected instance-key %q", res.InstanceKey)
		}

		_, instanceKey := snap.SplitInstanceName(instanceName)
		snapInfo.InstanceKey = instanceKey

		resources := make([]SnapResourceResult, 0, len(res.Snap.Resources))
		for _, r := range res.Snap.Resources {
			resources = append(resources, SnapResourceResult{
				DownloadInfo: downloadInfoFromStoreDownload(r.Download),
				Type:         r.Type,
				Name:         r.Name,
				Version:      r.Version,
				CreatedAt:    r.CreatedAt,
				Revision:     r.Revision,
			})
		}

		sars = append(sars, SnapActionResult{
			Info:            snapInfo,
			RedirectChannel: res.RedirectChannel,
			Resources:       resources,
		})
	}

	for _, errObj := range results.ErrorList {
		otherErrors = append(otherErrors, translateSnapActionError("", "", errObj.Code, errObj.Message, nil))
	}

	if len(refreshErrors)+len(installErrors)+len(downloadErrors) != 0 || len(results.Results) == 0 || len(otherErrors) != 0 {
		// normalize empty maps
		if len(refreshErrors) == 0 {
			refreshErrors = nil
		}
		if len(installErrors) == 0 {
			installErrors = nil
		}
		if len(downloadErrors) == 0 {
			downloadErrors = nil
		}
		return sars, ars, &SnapActionError{
			NoResults: len(results.Results) == 0,
			Refresh:   refreshErrors,
			Install:   installErrors,
			Download:  downloadErrors,
			Other:     otherErrors,
		}
	}

	return sars, ars, nil
}

func findRev(needle snap.Revision, haystack []snap.Revision) bool {
	for _, r := range haystack {
		if needle == r {
			return true
		}
	}
	return false
}

func reportFetchAssertionsError(res *snapActionResult, assertq AssertionQuery) error {
	// prefer to report the most unexpected error:
	// * errors not referring to an assertion (no valid type/primary-key)
	// are more unexpected than
	// * errors referring to a precise assertion that are not not-found
	// themselves more unexpected than
	// * not-found errors
	errIdx := -1
	errl := res.ErrorList
	carryingRef := func(ent *errorListEntry) bool {
		aType := asserts.Type(ent.Type)
		return aType != nil && aType.AcceptablePrimaryKey(ent.PrimaryKey)
	}
	carryingSeqKey := func(ent *errorListEntry) bool {
		aType := asserts.Type(ent.Type)
		return aType != nil && aType.SequenceForming() && len(ent.SequenceKey) == len(aType.PrimaryKey)-1
	}
	prio := func(ent *errorListEntry) int {
		if !carryingRef(ent) && !carryingSeqKey(ent) {
			return 2
		}
		if ent.Code != "not-found" {
			return 1
		}
		return 0
	}
	for i, ent := range errl {
		if errIdx == -1 {
			errIdx = i
			continue
		}
		prioOther := prio(&errl[errIdx])
		prioThis := prio(&ent)
		if prioThis > prioOther {
			errIdx = i
		}
	}
	rep := errl[errIdx]
	notFound := rep.Code == "not-found"
	switch {
	case carryingRef(&rep):
		ref := &asserts.Ref{Type: asserts.Type(rep.Type), PrimaryKey: rep.PrimaryKey}
		var err error
		if notFound {
			headers, _ := asserts.HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey)
			err = &asserts.NotFoundError{
				Type:    ref.Type,
				Headers: headers,
			}
		} else {
			err = fmt.Errorf("%s", rep.Message)
		}
		return assertq.AddError(err, ref)
	case carryingSeqKey(&rep):
		var err error
		atSeq := &asserts.AtSequence{Type: asserts.Type(rep.Type), SequenceKey: rep.SequenceKey}
		if notFound {
			headers, _ := asserts.HeadersFromSequenceKey(atSeq.Type, atSeq.SequenceKey)
			err = &asserts.NotFoundError{
				Type:    atSeq.Type,
				Headers: headers,
			}
		} else {
			err = fmt.Errorf("%s", rep.Message)
		}
		return assertq.AddSequenceError(err, atSeq)
	}

	return assertq.AddGroupingError(fmt.Errorf("%s", rep.Message), asserts.Grouping(res.Key))
}
