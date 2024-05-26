// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
)

// TransactionType says whether we want to treat each snap separately
// (the transaction is per snap) or whether to consider the call a
// single transaction so everything is reverted if it fails for just
// one snap. This applies to installs and updates, which can be done
// for multiple snaps in the same API call.
type TransactionType string

const (
	TransactionAllSnaps TransactionType = "all-snaps"
	TransactionPerSnap  TransactionType = "per-snap"
)

type SnapOptions struct {
	Channel          string          `json:"channel,omitempty"`
	Revision         string          `json:"revision,omitempty"`
	CohortKey        string          `json:"cohort-key,omitempty"`
	LeaveCohort      bool            `json:"leave-cohort,omitempty"`
	DevMode          bool            `json:"devmode,omitempty"`
	JailMode         bool            `json:"jailmode,omitempty"`
	Classic          bool            `json:"classic,omitempty"`
	Dangerous        bool            `json:"dangerous,omitempty"`
	IgnoreValidation bool            `json:"ignore-validation,omitempty"`
	IgnoreRunning    bool            `json:"ignore-running,omitempty"`
	Unaliased        bool            `json:"unaliased,omitempty"`
	Prefer           bool            `json:"prefer,omitempty"`
	Purge            bool            `json:"purge,omitempty"`
	Amend            bool            `json:"amend,omitempty"`
	Transaction      TransactionType `json:"transaction,omitempty"`
	QuotaGroupName   string          `json:"quota-group,omitempty"`
	ValidationSets   []string        `json:"validation-sets,omitempty"`
	Time             string          `json:"time,omitempty"`
	HoldLevel        string          `json:"hold-level,omitempty"`

	Users []string `json:"users,omitempty"`
}

func writeFieldBool(mw *multipart.Writer, key string, val bool) error {
	if !val {
		return nil
	}
	return mw.WriteField(key, "true")
}

type field struct {
	field string
	value bool
}

func writeFields(mw *multipart.Writer, fields []field) error {
	for _, fd := range fields {
		mylog.Check(writeFieldBool(mw, fd.field, fd.value))
	}

	return nil
}

func (opts *SnapOptions) writeModeFields(mw *multipart.Writer) error {
	fields := []field{
		{"devmode", opts.DevMode},
		{"classic", opts.Classic},
		{"jailmode", opts.JailMode},
		{"dangerous", opts.Dangerous},
	}
	return writeFields(mw, fields)
}

func (opts *SnapOptions) writeOptionFields(mw *multipart.Writer) error {
	fields := []field{
		{"ignore-running", opts.IgnoreRunning},
		{"unaliased", opts.Unaliased},
		{"prefer", opts.Prefer},
	}
	if opts.Transaction != "" {
		mylog.Check(mw.WriteField("transaction", string(opts.Transaction)))
	}
	if opts.QuotaGroupName != "" {
		mylog.Check(mw.WriteField("quota-group", opts.QuotaGroupName))
	}
	return writeFields(mw, fields)
}

type actionData struct {
	Action   string `json:"action"`
	Name     string `json:"name,omitempty"`
	SnapPath string `json:"snap-path,omitempty"`
	*SnapOptions
}

type multiActionData struct {
	Action         string          `json:"action"`
	Snaps          []string        `json:"snaps,omitempty"`
	Users          []string        `json:"users,omitempty"`
	Transaction    TransactionType `json:"transaction,omitempty"`
	IgnoreRunning  bool            `json:"ignore-running,omitempty"`
	Purge          bool            `json:"purge,omitempty"`
	ValidationSets []string        `json:"validation-sets,omitempty"`
	Time           string          `json:"time,omitempty"`
	HoldLevel      string          `json:"hold-level,omitempty"`
}

// Install adds the snap with the given name from the given channel (or
// the system default channel if not).
func (client *Client) Install(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("install", name, options)
}

func (client *Client) InstallMany(names []string, options *SnapOptions) (changeID string, err error) {
	return client.doMultiSnapAction("install", names, options)
}

// Remove removes the snap with the given name.
func (client *Client) Remove(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("remove", name, options)
}

func (client *Client) RemoveMany(names []string, options *SnapOptions) (changeID string, err error) {
	return client.doMultiSnapAction("remove", names, options)
}

// Refresh refreshes the snap with the given name (switching it to track
// the given channel if given).
func (client *Client) Refresh(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("refresh", name, options)
}

func (client *Client) RefreshMany(names []string, options *SnapOptions) (changeID string, err error) {
	return client.doMultiSnapAction("refresh", names, options)
}

func (client *Client) HoldRefreshes(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("hold", name, options)
}

func (client *Client) HoldRefreshesMany(names []string, options *SnapOptions) (changeID string, err error) {
	return client.doMultiSnapAction("hold", names, options)
}

func (client *Client) UnholdRefreshes(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("unhold", name, options)
}

func (client *Client) UnholdRefreshesMany(names []string, options *SnapOptions) (changeID string, err error) {
	return client.doMultiSnapAction("unhold", names, options)
}

func (client *Client) Enable(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("enable", name, options)
}

func (client *Client) Disable(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("disable", name, options)
}

// Revert rolls the snap back to the previous on-disk state
func (client *Client) Revert(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("revert", name, options)
}

// Switch moves the snap to a different channel without a refresh
func (client *Client) Switch(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("switch", name, options)
}

// SnapshotMany snapshots many snaps (all, if names empty) for many users (all, if users is empty).
func (client *Client) SnapshotMany(names []string, users []string) (setID uint64, changeID string, err error) {
	result, changeID := mylog.Check3(client.doMultiSnapActionFull("snapshot", names, &SnapOptions{Users: users}))

	if len(result) == 0 {
		return 0, "", fmt.Errorf("server result does not contain snapshot set identifier")
	}
	var x struct {
		SetID uint64 `json:"set-id"`
	}
	mylog.Check(json.Unmarshal(result, &x))

	return x.SetID, changeID, nil
}

var ErrDangerousNotApplicable = fmt.Errorf("dangerous option only meaningful when installing from a local file")

func (client *Client) doSnapAction(actionName string, snapName string, options *SnapOptions) (changeID string, err error) {
	if options != nil && options.Dangerous {
		return "", ErrDangerousNotApplicable
	}
	action := actionData{
		Action:      actionName,
		SnapOptions: options,
	}
	data := mylog.Check2(json.Marshal(&action))

	path := fmt.Sprintf("/v2/snaps/%s", snapName)

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", path, nil, headers, bytes.NewBuffer(data))
}

func (client *Client) doMultiSnapAction(actionName string, snaps []string, options *SnapOptions) (changeID string, err error) {
	_, changeID = mylog.Check3(client.doMultiSnapActionFull(actionName, snaps, options))

	return changeID, err
}

func (client *Client) doMultiSnapActionFull(actionName string, snaps []string, options *SnapOptions) (result json.RawMessage, changeID string, err error) {
	action := multiActionData{
		Action: actionName,
		Snaps:  snaps,
	}
	if options != nil {
		// TODO: consider returning error when options.Dangerous is set
		action.Users = options.Users
		action.Transaction = options.Transaction
		action.IgnoreRunning = options.IgnoreRunning
		action.Purge = options.Purge
		action.ValidationSets = options.ValidationSets
		action.Time = options.Time
		action.HoldLevel = options.HoldLevel
	}

	data := mylog.Check2(json.Marshal(&action))

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsyncFull("POST", "/v2/snaps", nil, headers, bytes.NewBuffer(data), nil)
}

// InstallPath sideloads the snap with the given path under optional provided name,
// returning the UUID of the background operation upon success.
func (client *Client) InstallPath(path, name string, options *SnapOptions) (changeID string, err error) {
	f := mylog.Check2(os.Open(path))

	action := actionData{
		Action:      "install",
		Name:        name,
		SnapPath:    path,
		SnapOptions: options,
	}

	return client.sendLocalSnaps([]string{path}, []*os.File{f}, action)
}

// InstallPathMany sideloads the snaps with the given paths,
// returning the UUID of the background operation upon success.
func (client *Client) InstallPathMany(paths []string, options *SnapOptions) (changeID string, err error) {
	action := actionData{
		Action:      "install",
		SnapOptions: options,
	}

	var files []*os.File
	for _, path := range paths {
		f := mylog.Check2(os.Open(path))

		files = append(files, f)
	}

	return client.sendLocalSnaps(paths, files, action)
}

func (client *Client) sendLocalSnaps(paths []string, files []*os.File, action actionData) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go sendSnapFiles(paths, files, pw, mw, &action)

	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	_, changeID := mylog.Check3(client.doAsyncFull("POST", "/v2/snaps", nil, headers, pr, doNoTimeoutAndRetry))
	return changeID, err
}

// Try
func (client *Client) Try(path string, options *SnapOptions) (changeID string, err error) {
	if options == nil {
		options = &SnapOptions{}
	}
	if options.Dangerous {
		return "", ErrDangerousNotApplicable
	}

	buf := bytes.NewBuffer(nil)
	mw := multipart.NewWriter(buf)
	mw.WriteField("action", "try")
	mw.WriteField("snap-path", path)
	options.writeModeFields(mw)
	mw.Close()

	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	return client.doAsync("POST", "/v2/snaps", nil, headers, buf)
}

func sendSnapFiles(paths []string, files []*os.File, pw *io.PipeWriter, mw *multipart.Writer, action *actionData) {
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	if action.SnapOptions == nil {
		action.SnapOptions = &SnapOptions{}
	}

	type field struct {
		name  string
		value string
	}

	fields := []field{{"action", action.Action}}
	if len(paths) == 1 {
		fields = append(fields, []field{
			{"name", action.Name},
			{"snap-path", action.SnapPath},
			{"channel", action.Channel},
		}...)
	}

	for _, s := range fields {
		if s.value == "" {
			continue
		}
		mylog.Check(mw.WriteField(s.name, s.value))

	}
	mylog.Check(action.writeModeFields(mw))
	mylog.Check(action.writeOptionFields(mw))

	for i, file := range files {
		path := paths[i]
		fw := mylog.Check2(mw.CreateFormFile("snap", filepath.Base(path)))

		_ = mylog.Check2(io.Copy(fw, file))

	}

	mw.Close()
	pw.Close()
}

type snapRevisionOptions struct {
	Channel   string `json:"channel,omitempty"`
	Revision  string `json:"revision,omitempty"`
	CohortKey string `json:"cohort-key,omitempty"`
}

type downloadAction struct {
	SnapName string `json:"snap-name"`

	snapRevisionOptions

	HeaderPeek  bool   `json:"header-peek,omitempty"`
	ResumeToken string `json:"resume-token,omitempty"`
}

type DownloadInfo struct {
	SuggestedFileName string
	Size              int64
	Sha3_384          string
	ResumeToken       string
}

type DownloadOptions struct {
	SnapOptions

	HeaderPeek  bool
	ResumeToken string
	Resume      int64
}

// Download will stream the given snap to the client
func (client *Client) Download(name string, options *DownloadOptions) (dlInfo *DownloadInfo, r io.ReadCloser, err error) {
	if options == nil {
		options = &DownloadOptions{}
	}
	action := downloadAction{
		SnapName: name,
		snapRevisionOptions: snapRevisionOptions{
			Channel:   options.Channel,
			CohortKey: options.CohortKey,
			Revision:  options.Revision,
		},
		HeaderPeek:  options.HeaderPeek,
		ResumeToken: options.ResumeToken,
	}
	data := mylog.Check2(json.Marshal(&action))

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if options.Resume > 0 {
		headers["range"] = fmt.Sprintf("bytes: %d-", options.Resume)
	}

	// no deadline for downloads
	ctx := context.Background()
	rsp := mylog.Check2(client.raw(ctx, "POST", "/v2/download", nil, headers, bytes.NewBuffer(data)))

	if rsp.StatusCode != 200 {
		var r response
		defer rsp.Body.Close()
		mylog.Check(decodeInto(rsp.Body, &r))

		return nil, nil, r.err(client, rsp.StatusCode)
	}
	matches := contentDispositionMatcher(rsp.Header.Get("Content-Disposition"))
	if matches == nil || matches[1] == "" {
		return nil, nil, fmt.Errorf("cannot determine filename")
	}

	dlInfo = &DownloadInfo{
		SuggestedFileName: matches[1],
		Size:              rsp.ContentLength,
		Sha3_384:          rsp.Header.Get("Snap-Sha3-384"),
		ResumeToken:       rsp.Header.Get("Snap-Download-Token"),
	}

	return dlInfo, rsp.Body, nil
}
