// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	DevMode          bool   `json:"devmode,omitempty"`
	JailMode         bool   `json:"jailmode,omitempty"`
	Classic          bool   `json:"classic,omitempty"`
	Dangerous        bool   `json:"dangerous,omitempty"`
	IgnoreValidation bool   `json:"ignore-validation,omitempty"`
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
		if !o.b {
			continue
		}
		err := mw.WriteField(o.f, "true")
		if err != nil {
			return err
		}
	}

	return nil
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
	action := multiActionData{
		Action: actionName,
		Snaps:  snaps,
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal multi-snap action: %s", err)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/snaps", nil, headers, bytes.NewBuffer(data))
}

// InstallPath sideloads the snap with the given path, returning the UUID
// of the background operation upon success.
func (client *Client) InstallPath(path string, options *SnapOptions) (changeID string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open: %q", path)
	}

	action := actionData{
		Action:      "install",
		SnapPath:    path,
		SnapOptions: options,
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go sendSnapFile(path, f, pw, mw, &action)

	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	return client.doAsync("POST", "/v2/snaps", nil, headers, pr)
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

func sendSnapFile(snapPath string, snapFile *os.File, pw *io.PipeWriter, mw *multipart.Writer, action *actionData) {
	defer snapFile.Close()

	if action.SnapOptions == nil {
		action.SnapOptions = &SnapOptions{}
	}
	fields := []struct {
		name  string
		value string
	}{
		{"action", action.Action},
		{"name", action.Name},
		{"snap-path", action.SnapPath},
		{"channel", action.Channel},
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

	fw, err := mw.CreateFormFile("snap", filepath.Base(snapPath))
	if err != nil {
		pw.CloseWithError(err)
		return
	}

	_, err = io.Copy(fw, snapFile)
	if err != nil {
		pw.CloseWithError(err)
		return
	}

	mw.Close()
	pw.Close()
}
