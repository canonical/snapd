// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
)

type SnapOptions struct {
	Channel          string `json:"channel,omitempty"`
	Revision         string `json:"revision,omitempty"`
	CohortKey        string `json:"cohort-key,omitempty"`
	LeaveCohort      bool   `json:"leave-cohort,omitempty"`
	DevMode          bool   `json:"devmode,omitempty"`
	JailMode         bool   `json:"jailmode,omitempty"`
	Classic          bool   `json:"classic,omitempty"`
	Dangerous        bool   `json:"dangerous,omitempty"`
	IgnoreValidation bool   `json:"ignore-validation,omitempty"`
	IgnoreRunning    bool   `json:"ignore-running,omitempty"`
	Unaliased        bool   `json:"unaliased,omitempty"`
	Purge            bool   `json:"purge,omitempty"`
	Amend            bool   `json:"amend,omitempty"`

	Users []string `json:"users,omitempty"`
}

func writeFieldBool(mw *multipart.Writer, key string, val bool) error {
	if !val {
		return nil
	}
	return mw.WriteField(key, "true")
}

func (opts *SnapOptions) writeModeFields(mw *multipart.Writer) error {
	fields := []struct {
		f string
		b bool
	}{
		{"devmode", opts.DevMode},
		{"classic", opts.Classic},
		{"jailmode", opts.JailMode},
		{"dangerous", opts.Dangerous},
	}
	for _, o := range fields {
		if err := writeFieldBool(mw, o.f, o.b); err != nil {
			return err
		}
	}

	return nil
}

func (opts *SnapOptions) writeOptionFields(mw *multipart.Writer) error {
	if err := writeFieldBool(mw, "ignore-running", opts.IgnoreRunning); err != nil {
		return err
	}
	return writeFieldBool(mw, "unaliased", opts.Unaliased)
}

type actionData struct {
	Action   string `json:"action"`
	Name     string `json:"name,omitempty"`
	SnapPath string `json:"snap-path,omitempty"`
	*SnapOptions
}

type multiActionData struct {
	Action string   `json:"action"`
	Snaps  []string `json:"snaps,omitempty"`
	Users  []string `json:"users,omitempty"`
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
	result, changeID, err := client.doMultiSnapActionFull("snapshot", names, &SnapOptions{Users: users})
	if err != nil {
		return 0, "", err
	}
	if len(result) == 0 {
		return 0, "", fmt.Errorf("server result does not contain snapshot set identifier")
	}
	var x struct {
		SetID uint64 `json:"set-id"`
	}
	if err := json.Unmarshal(result, &x); err != nil {
		return 0, "", err
	}
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
	data, err := json.Marshal(&action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal snap action: %s", err)
	}
	path := fmt.Sprintf("/v2/snaps/%s", snapName)

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", path, nil, headers, bytes.NewBuffer(data))
}

func (client *Client) doMultiSnapAction(actionName string, snaps []string, options *SnapOptions) (changeID string, err error) {
	if options != nil {
		return "", fmt.Errorf("cannot use options for multi-action") // (yet)
	}
	_, changeID, err = client.doMultiSnapActionFull(actionName, snaps, options)

	return changeID, err
}

func (client *Client) doMultiSnapActionFull(actionName string, snaps []string, options *SnapOptions) (result json.RawMessage, changeID string, err error) {
	action := multiActionData{
		Action: actionName,
		Snaps:  snaps,
	}
	if options != nil {
		action.Users = options.Users
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return nil, "", fmt.Errorf("cannot marshal multi-snap action: %s", err)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsyncFull("POST", "/v2/snaps", nil, headers, bytes.NewBuffer(data), nil)
}

// InstallPath sideloads the snap with the given path under optional provided name,
// returning the UUID of the background operation upon success.
func (client *Client) InstallPath(path, name string, options *SnapOptions) (changeID string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open %q: %w", path, err)
	}

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
		f, err := os.Open(path)
		if err != nil {
			for _, openFile := range files {
				openFile.Close()
			}
			return "", fmt.Errorf("cannot open %q: %w", path, err)
		}

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

	_, changeID, err := client.doAsyncFull("POST", "/v2/snaps", nil, headers, pr, doNoTimeoutAndRetry)
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
			{"channel", action.Channel}}...)
	}

	for _, s := range fields {
		if s.value == "" {
			continue
		}
		if err := mw.WriteField(s.name, s.value); err != nil {
			pw.CloseWithError(err)
			return
		}
	}

	if err := action.writeModeFields(mw); err != nil {
		pw.CloseWithError(err)
		return
	}

	if err := action.writeOptionFields(mw); err != nil {
		pw.CloseWithError(err)
		return
	}

	for i, file := range files {
		path := paths[i]
		fw, err := mw.CreateFormFile("snap", filepath.Base(path))
		if err != nil {
			pw.CloseWithError(err)
			return
		}

		_, err = io.Copy(fw, file)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
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
	data, err := json.Marshal(&action)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal snap action: %s", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if options.Resume > 0 {
		headers["range"] = fmt.Sprintf("bytes: %d-", options.Resume)
	}

	// no deadline for downloads
	ctx := context.Background()
	rsp, err := client.raw(ctx, "POST", "/v2/download", nil, headers, bytes.NewBuffer(data))
	if err != nil {
		return nil, nil, err
	}

	if rsp.StatusCode != 200 {
		var r response
		defer rsp.Body.Close()
		if err := decodeInto(rsp.Body, &r); err != nil {
			return nil, nil, err
		}
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
