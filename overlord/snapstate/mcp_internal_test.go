// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestMCPToolTypedHelpers(t *testing.T) {
	st := state.New(nil)

	if _, ok := (listSnapsTool{}).ArgsType().(*listSnapsArgs); !ok {
		t.Fatal("listSnapsTool ArgsType returned unexpected type")
	}
	if _, ok := (listSnapsTool{}).ResultType().(*listSnapsResult); !ok {
		t.Fatal("listSnapsTool ResultType returned unexpected type")
	}
	if err := (listSnapsTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listSnapsTool ValidateArgs should reject wrong type")
	}
	if _, err := (listSnapsTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listSnapsTool CallWithArgs should reject wrong type")
	}

	if err := (getSnapTool{}).ValidateArgs(&getSnapArgs{}); err == nil {
		t.Fatal("getSnapTool ValidateArgs should reject empty snap name")
	}
	if _, err := (getSnapTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("getSnapTool CallWithArgs should reject wrong type")
	}

	if err := (searchStoreSnapsTool{}).ValidateArgs(&searchStoreSnapsArgs{}); err == nil {
		t.Fatal("searchStoreSnapsTool ValidateArgs should reject empty name")
	}
	if _, err := (searchStoreSnapsTool{}).CallWithArgs(context.Background(), st, &searchStoreSnapsArgs{Name: "  "}); err == nil {
		t.Fatal("searchStoreSnapsTool CallWithArgs should reject empty query")
	}

	if err := (getStoreSnapTool{}).ValidateArgs(&getStoreSnapArgs{}); err == nil {
		t.Fatal("getStoreSnapTool ValidateArgs should reject empty snap name")
	}
	if _, err := (getStoreSnapTool{}).CallWithArgs(context.Background(), st, &getStoreSnapArgs{SnapName: "  "}); err == nil {
		t.Fatal("getStoreSnapTool CallWithArgs should reject empty snap name")
	}

	if err := (listChangesTool{}).ValidateArgs(&listChangesArgs{Select: "bogus"}); err == nil {
		t.Fatal("listChangesTool ValidateArgs should reject invalid select")
	}
	if err := (listChangesTool{}).ValidateArgs(&listChangesArgs{Kind: "bogus"}); err == nil {
		t.Fatal("listChangesTool ValidateArgs should reject invalid kind")
	}
	if err := (listChangesTool{}).ValidateArgs(&listChangesArgs{Status: "bogus"}); err == nil {
		t.Fatal("listChangesTool ValidateArgs should reject invalid status")
	}
	now := time.Now()
	later := now.Add(-time.Hour)
	if err := (listChangesTool{}).ValidateArgs(&listChangesArgs{Since: &now, Until: &later}); err == nil {
		t.Fatal("listChangesTool ValidateArgs should reject reversed time range")
	}
	if _, err := (listChangesTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listChangesTool CallWithArgs should reject wrong type")
	}

	if err := (listChangeTasksTool{}).ValidateArgs(&listChangeTasksArgs{}); err == nil {
		t.Fatal("listChangeTasksTool ValidateArgs should reject empty change id")
	}
	if _, err := (listChangeTasksTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listChangeTasksTool CallWithArgs should reject wrong type")
	}

	if err := (listServicesTool{}).ValidateArgs(struct{}{}); err == nil {
		t.Fatal("listServicesTool ValidateArgs should reject wrong type")
	}
	if _, err := (listServicesTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("listServicesTool CallWithArgs should reject wrong type")
	}

	if err := (getServiceLogsTool{}).ValidateArgs(&getServiceLogsArgs{}); err == nil {
		t.Fatal("getServiceLogsTool ValidateArgs should reject empty service name")
	}
	if err := (getServiceLogsTool{}).ValidateArgs(&getServiceLogsArgs{ServiceName: "snap.app"}); err != nil {
		t.Fatalf("getServiceLogsTool ValidateArgs should allow omitted lines value: %v", err)
	}
	if err := (getServiceLogsTool{}).ValidateArgs(&getServiceLogsArgs{ServiceName: "snap"}); err == nil {
		t.Fatal("getServiceLogsTool ValidateArgs should reject malformed service name")
	}
	if err := (getServiceLogsTool{}).ValidateArgs(&getServiceLogsArgs{ServiceName: "snap.app", Lines: -2}); err == nil {
		t.Fatal("getServiceLogsTool ValidateArgs should reject invalid line count")
	}
	if err := (getServiceLogsTool{}).ValidateArgs(&getServiceLogsArgs{ServiceName: "snap.app", Since: &now, Until: &later}); err == nil {
		t.Fatal("getServiceLogsTool ValidateArgs should reject reversed time range")
	}
	if _, err := (getServiceLogsTool{}).CallWithArgs(context.Background(), st, struct{}{}); err == nil {
		t.Fatal("getServiceLogsTool CallWithArgs should reject wrong type")
	}
}

func TestMCPStateHelpers(t *testing.T) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install-snap", "install snap")
	chg.Set("snap-names", []string{"snap-a.app", "snap-b"})
	task := st.NewTask("download-snap", "Download snap")
	task.SetProgress("downloading", 1, 2)
	task.Logf("line one")
	chg.AddTask(task)
	st.Unlock()

	if !matchesChangeSelect(chg, "all") {
		t.Fatal("matchesChangeSelect should accept all")
	}
	if !matchesChangeSelect(chg, "in-progress") {
		t.Fatal("matchesChangeSelect should match in-progress change")
	}
	if matchesChangeSelect(chg, "ready") {
		t.Fatal("matchesChangeSelect should not match non-ready change as ready")
	}
	if matchesChangeSelect(chg, "bogus") {
		t.Fatal("matchesChangeSelect should reject unknown selector")
	}

	st.Lock()
	chg.SetStatus(state.ErrorStatus)
	if !matchesChangeStatus(chg, "failed") {
		st.Unlock()
		t.Fatal("matchesChangeStatus should treat failed as error")
	}
	if matchesChangeStatus(chg, "done") {
		st.Unlock()
		t.Fatal("matchesChangeStatus should not match wrong status")
	}

	snapNames := changeSnapNames(chg)
	if len(snapNames) != 2 {
		st.Unlock()
		t.Fatalf("expected 2 snap names, got %d", len(snapNames))
	}
	if !containsSnapName(snapNames, "snap-a") {
		st.Unlock()
		t.Fatal("containsSnapName should match snap name before app suffix")
	}
	if containsSnapName(snapNames, "snap-c") {
		st.Unlock()
		t.Fatal("containsSnapName should not match missing snap")
	}

	item := changeToItem(chg, snapNames)
	if item.ID != chg.ID() || item.Kind != "install-snap" {
		st.Unlock()
		t.Fatalf("unexpected change item: %#v", item)
	}
	if len(item.SnapNames) != 2 {
		st.Unlock()
		t.Fatalf("expected snap names in change item, got %#v", item.SnapNames)
	}

	changeMap := changeToMap(chg, snapNames)
	if changeMap["id"] != chg.ID() {
		st.Unlock()
		t.Fatalf("unexpected change map id: %#v", changeMap)
	}
	if _, ok := changeMap["snap_names"]; !ok {
		st.Unlock()
		t.Fatalf("expected snap_names in change map: %#v", changeMap)
	}

	taskItem := taskToItem(task)
	if taskItem.Progress.Label != "downloading" || len(taskItem.Log) != 1 {
		st.Unlock()
		t.Fatalf("unexpected task item: %#v", taskItem)
	}
	taskMap := taskToMap(task)
	if taskMap["kind"] != "download-snap" {
		st.Unlock()
		t.Fatalf("unexpected task map: %#v", taskMap)
	}
	if _, ok := taskMap["log"]; !ok {
		st.Unlock()
		t.Fatalf("expected task log in map: %#v", taskMap)
	}
	st.Unlock()
}

func TestListSnapsToResultFiltersByNameCaseInsensitiveSubstring(t *testing.T) {
	snaps := []snapResult{
		{info: &snap.Info{SuggestedName: "core-snap", SideInfo: snap.SideInfo{RealName: "core-snap"}}},
		{info: &snap.Info{SuggestedName: "Hello-World", SideInfo: snap.SideInfo{RealName: "Hello-World"}}},
	}

	result := listSnapsToResult(snaps, "heLLo")
	if len(result.Snaps) != 1 {
		t.Fatalf("expected one snap after filter, got %d", len(result.Snaps))
	}
	if result.Snaps[0].Name != "Hello-World" {
		t.Fatalf("unexpected filtered snap: %#v", result.Snaps[0])
	}
}

func TestListSnapsToResultNoFilterIncludesAll(t *testing.T) {
	snaps := []snapResult{
		{info: &snap.Info{SuggestedName: "snap-a", SideInfo: snap.SideInfo{RealName: "snap-a"}}},
		{info: &snap.Info{SuggestedName: "snap-b", SideInfo: snap.SideInfo{RealName: "snap-b"}}},
	}

	result := listSnapsToResult(snaps, "")
	if len(result.Snaps) != 2 {
		t.Fatalf("expected all snaps without filter, got %d", len(result.Snaps))
	}
}

func TestIntArg(t *testing.T) {
	if _, err := intArg(map[string]any{}, "lines"); err == nil {
		t.Fatal("intArg should require key")
	}
	if _, err := intArg(map[string]any{"lines": "1"}, "lines"); err == nil {
		t.Fatal("intArg should reject non-number")
	}
	if _, err := intArg(map[string]any{"lines": 1.5}, "lines"); err == nil {
		t.Fatal("intArg should reject fractional values")
	}
	v, err := intArg(map[string]any{"lines": 2.0}, "lines")
	if err != nil || v != 2 {
		t.Fatalf("unexpected intArg result: %d, %v", v, err)
	}
}

func TestSnapInfoResourceAndJSONString(t *testing.T) {
	if got := jsonString(map[string]any{"hello": "world"}); got != `{"hello":"world"}` {
		t.Fatalf("unexpected jsonString output: %q", got)
	}

	resource := snapInfoResource{}
	if resource.Pattern() != "/info/" {
		t.Fatalf("unexpected pattern: %q", resource.Pattern())
	}
	if resource.Descriptor().URI != "snap://info/{snap}" {
		t.Fatalf("unexpected descriptor: %#v", resource.Descriptor())
	}

	st := state.New(nil)
	restore := MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		return &snap.Info{
			SuggestedName:   name,
			Version:         "1.0",
			OriginalTitle:   "Title",
			OriginalSummary: "Summary",
			Publisher:       snap.StoreAccount{Username: "publisher"},
			SideInfo:        *si,
		}, nil
	})
	defer restore()

	st.Lock()
	Set(st, "test-snap", &SnapState{
		Sequence:        sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{sequence.NewRevisionSideState(&snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)}, nil)}},
		Current:         snap.R(1),
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	st.Unlock()

	missingReq := httptest.NewRequest("GET", "http://example/info/missing", nil)
	if _, err := resource.Read(context.Background(), st, missingReq); err == nil {
		t.Fatal("snapInfoResource should report missing snap")
	}

	req := httptest.NewRequest("GET", "http://example/info/test-snap", nil)
	result, err := resource.Read(context.Background(), st, req)
	if err != nil {
		t.Fatalf("snapInfoResource returned error: %v", err)
	}
	contents := result.(map[string]any)["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("unexpected contents: %#v", result)
	}
	if contents[0]["uri"] != "snap://info/test-snap" {
		t.Fatalf("unexpected resource uri: %#v", contents[0])
	}
}

func TestStoreMappingHelpers(t *testing.T) {
	info := &snap.Info{
		SuggestedName:       "store-snap",
		Version:             "1.2",
		Architectures:       []string{"amd64"},
		Base:                "core24",
		Confinement:         snap.StrictConfinement,
		License:             "GPL-3.0",
		Tracks:              []string{"latest"},
		OriginalTitle:       "Store Snap",
		OriginalSummary:     "Store Summary",
		OriginalDescription: "Store Description",
		Publisher:           snap.StoreAccount{Username: "publisher", DisplayName: "Publisher Name"},
		StoreURL:            "https://snapcraft.io/store-snap",
		OriginalLinks:       map[string][]string{"website": {"https://example.com"}, "contact": {"mailto:test@example.com"}},
		Channels:            map[string]*snap.ChannelSnapInfo{"latest/stable": {Channel: "latest/stable", Version: "1.2", Revision: snap.R(7), Confinement: snap.StrictConfinement, ReleasedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)}},
		SideInfo:            snap.SideInfo{RealName: "store-snap", Revision: snap.R(7), SnapID: "snapid-1"},
		SnapType:            snap.TypeApp,
	}

	summary := storeSnapSummaryToMap(info)
	if summary["name"] != "store-snap" || summary["snap_id"] != "snapid-1" {
		t.Fatalf("unexpected store snap summary: %#v", summary)
	}

	details := storeSnapDetailsToMap(info)
	if details["publisher"] != "Publisher Name" || details["website"] != "https://example.com" || details["contact"] != "mailto:test@example.com" {
		t.Fatalf("unexpected store snap details: %#v", details)
	}
	channels := details["channels"].(map[string]map[string]any)
	if channels["latest/stable"]["revision"] != 7 {
		t.Fatalf("unexpected channel details: %#v", channels)
	}

	if firstStoreLink(nil, "website") != "" {
		t.Fatal("firstStoreLink should return empty string for missing links")
	}
	if firstStoreLink(info.Links(), "website") != "https://example.com" {
		t.Fatal("firstStoreLink should return first link value")
	}

	if statusFromSnapState(&SnapState{Active: true}) != "active" {
		t.Fatal("statusFromSnapState should report active")
	}
	if statusFromSnapState(&SnapState{}) != "installed" {
		t.Fatal("statusFromSnapState should report installed for inactive snaps")
	}
}

func TestStoreFromStateErrors(t *testing.T) {
	if _, err := storeFromState(state.New(nil)); err == nil {
		t.Fatal("storeFromState should fail when no store service is available")
	}
}

func TestDecodeJournalEntriesAndPriority(t *testing.T) {
	entries, err := decodeJournalEntries(strings.NewReader(
		`{"__REALTIME_TIMESTAMP":"1700000000000000","MESSAGE":"hello","SYSLOG_IDENTIFIER":"svc","_PID":"123","PRIORITY":"4"}` + "\n" +
			`{"MESSAGE":["line1","line2"],"SYSLOG_PID":"321","PRIORITY":["2"]}` + "\n"))
	if err != nil {
		t.Fatalf("decodeJournalEntries returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected decoded entries: %#v", entries)
	}
	if entries[0]["message"] != "hello" || entries[0]["sid"] != "svc" || entries[0]["pid"] != "123" || entries[0]["priority"] != 4 {
		t.Fatalf("unexpected first log entry: %#v", entries[0])
	}
	if entries[1]["message"] != "line1\nline2" || entries[1]["sid"] != "-" || entries[1]["pid"] != "321" || entries[1]["priority"] != 2 {
		t.Fatalf("unexpected second log entry: %#v", entries[1])
	}
	if _, ok := entries[0]["timestamp"].(time.Time); !ok {
		t.Fatalf("expected time.Time timestamp in first entry, got %#v", entries[0]["timestamp"])
	}
	if _, ok := entries[1]["timestamp"].(time.Time); !ok {
		t.Fatalf("expected time.Time timestamp in second entry, got %#v", entries[1]["timestamp"])
	}

	if _, err := decodeJournalEntries(strings.NewReader(`{"MESSAGE":`)); err == nil {
		t.Fatal("decodeJournalEntries should reject invalid JSON")
	}

	if _, ok := journalPriority(nil); ok {
		t.Fatal("journalPriority should report missing priority")
	}
}

func TestServiceHelpers(t *testing.T) {
	serviceInfo, err := snap.InfoFromSnapYaml([]byte(`name: svc-snap
version: 1
apps:
  daemon:
    command: bin/run
    daemon: simple
  cli:
    command: bin/cli
`))
	if err != nil {
		t.Fatalf("cannot parse service snap yaml: %v", err)
	}
	nonServiceInfo, err := snap.InfoFromSnapYaml([]byte(`name: other-snap
version: 1
apps:
  app:
    command: bin/app
`))
	if err != nil {
		t.Fatalf("cannot parse non-service snap yaml: %v", err)
	}

	st := state.New(nil)
	restore := MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "svc-snap":
			return serviceInfo, nil
		case "other-snap":
			return nonServiceInfo, nil
		default:
			return nil, ErrNoCurrent
		}
	})
	defer restore()

	st.Lock()
	Set(st, "svc-snap", &SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "svc-snap", Revision: snap.R(1)}, nil),
		}},
		Current: snap.R(1),
		Active:  true,
	})
	Set(st, "other-snap", &SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "other-snap", Revision: snap.R(1)}, nil),
		}},
		Current: snap.R(1),
		Active:  true,
	})
	st.Unlock()

	services, err := listServicesFromState(st, "", "")
	if err != nil {
		t.Fatalf("listServicesFromState returned error: %v", err)
	}
	if len(services) != 1 || services[0].ServiceName != "svc-snap.daemon" {
		t.Fatalf("unexpected services: %#v", services)
	}

	filtered, err := listServicesFromState(st, "svc-snap", "daemon")
	if err != nil || len(filtered) != 1 {
		t.Fatalf("unexpected filtered services: %#v, err=%v", filtered, err)
	}

	app, err := serviceFromState(st, "svc-snap.daemon")
	if err != nil {
		t.Fatalf("serviceFromState returned error: %v", err)
	}
	if app.Name != "daemon" || !app.IsService() {
		t.Fatalf("unexpected app: %#v", app)
	}

	if _, err := serviceFromState(st, "badname"); err == nil {
		t.Fatal("serviceFromState should reject malformed names")
	}
	if _, err := serviceFromState(st, "svc-snap.missing"); err == nil {
		t.Fatal("serviceFromState should reject missing service")
	}
	if _, err := serviceFromState(st, "other-snap.app"); err == nil {
		t.Fatal("serviceFromState should reject non-service app")
	}
}
