// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package internal

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func renderListenStream(socket *snap.SocketInfo) string {
	s := socket.App.Snap
	listenStream := socket.ListenStream
	switch socket.App.DaemonScope {
	case snap.SystemDaemon:
		listenStream = strings.Replace(listenStream, "$SNAP_DATA", s.DataDir(), -1)
		// TODO: when we support User/Group in the generated
		// systemd unit, adjust this accordingly
		serviceUserUid := sys.UserID(0)
		runtimeDir := s.UserXdgRuntimeDir(serviceUserUid)
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", runtimeDir, -1)
		listenStream = strings.Replace(listenStream, "$SNAP_COMMON", s.CommonDataDir(), -1)
	case snap.UserDaemon:
		// TODO: use SnapDirOpts here. User daemons are also an experimental
		// feature so, for simplicity, we can not pass opts here for now
		listenStream = strings.Replace(listenStream, "$SNAP_USER_DATA", s.UserDataDir("%h", nil), -1)
		listenStream = strings.Replace(listenStream, "$SNAP_USER_COMMON", s.UserCommonDataDir("%h", nil), -1)
		// FIXME: find some way to share code with snap.UserXdgRuntimeDir()
		listenStream = strings.Replace(listenStream, "$XDG_RUNTIME_DIR", fmt.Sprintf("%%t/snap.%s", s.InstanceName()), -1)
	default:
		panic("unknown snap.DaemonScope")
	}
	return listenStream
}

func generateSnapServiceSocketUnitFile(appInfo *snap.AppInfo, socketName string) []byte {
	socketTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket {{.SocketName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Socket]
Service={{.ServiceFileName}}
FileDescriptorName={{.SocketInfo.Name}}
ListenStream={{.ListenStream}}
{{- if .SocketInfo.SocketMode}}
SocketMode={{.SocketInfo.SocketMode | printf "%04o"}}
{{- end}}

[Install]
WantedBy={{.SocketsTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("socket-wrapper").Parse(socketTemplate))

	socket := appInfo.Sockets[socketName]
	listenStream := renderListenStream(socket)
	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		SocketsTarget   string
		MountUnit       string
		SocketName      string
		SocketInfo      *snap.SocketInfo
		ListenStream    string
	}{
		App:             appInfo,
		ServiceFileName: filepath.Base(appInfo.ServiceFile()),
		SocketsTarget:   systemd.SocketsTarget,
		SocketName:      socketName,
		SocketInfo:      socket,
		ListenStream:    listenStream,
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}
	mylog.Check(t.Execute(&templateOut, wrapperData))
	// this can never happen, except we forget a variable

	return templateOut.Bytes()
}

func GenerateSnapSocketUnitFiles(app *snap.AppInfo) (map[string][]byte, error) {
	mylog.Check(snap.ValidateApp(app))

	socketFiles := make(map[string][]byte)
	for name := range app.Sockets {
		socketFiles[name] = generateSnapServiceSocketUnitFile(app, name)
	}
	return socketFiles, nil
}
