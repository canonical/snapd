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

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

const (
	maxLenUnixSocketAddress = len(unix.RawSockaddrUnix{}.Path)
	// maxLenUnixAbstractSocketAddress includes the leading null byte used by
	// abstract AF_UNIX addresses.
	maxLenUnixAbstractSocketAddress = maxLenUnixSocketAddress
	// maxLenUnixPathSocketAddress excludes the trailing null byte required by
	// pathname AF_UNIX addresses.
	maxLenUnixPathSocketAddress = maxLenUnixSocketAddress - 1
)

func renderListenStream(socket *snap.SocketInfo) (string, error) {
	s := socket.App.Snap
	listenStream := socket.ListenStream

	switch listenStream[0] {
	case '@': // unix abstract socket
		prefixSnapName := fmt.Sprintf("@snap.%s.", s.SnapName())
		prefixInstanceName := fmt.Sprintf("@snap.%s.", s.InstanceName())
		// ValidateApp has already enforced the "@snap.<snap-name>." prefix in AppInfo.
		// For parallel installs, the abstract socket address is remapped to
		// "@snap.<instance-name>." prefix.
		// Clients must connect using the instance-qualified address.
		listenStream = strings.Replace(listenStream, prefixSnapName, prefixInstanceName, 1)
		if len(listenStream) > maxLenUnixAbstractSocketAddress {
			return "", fmt.Errorf("abstract socket address %q is too long (%d bytes), maximum is %d",
				listenStream, len(listenStream), maxLenUnixAbstractSocketAddress)
		}
		return listenStream, nil
	case '/', '$': // unix path socket
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
		if len(listenStream) > maxLenUnixPathSocketAddress {
			return "", fmt.Errorf("socket path %q is too long (%d bytes), maximum is %d",
				listenStream, len(listenStream), maxLenUnixPathSocketAddress)
		}
		return listenStream, nil
	default: // network socket
		return listenStream, nil
	}
}

func generateSnapServiceSocketUnitFile(appInfo *snap.AppInfo, socketName string) ([]byte, error) {
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
	listenStream, err := renderListenStream(socket)
	if err != nil {
		return nil, err
	}
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

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}

func GenerateSnapSocketUnitFiles(app *snap.AppInfo) (map[string][]byte, error) {
	if err := snap.ValidateApp(app); err != nil {
		return nil, err
	}

	socketFiles := make(map[string][]byte)
	for name := range app.Sockets {
		socketFile, err := generateSnapServiceSocketUnitFile(app, name)
		if err != nil {
			return nil, fmt.Errorf("cannot generate socket unit for socket %q: %v", name, err)
		}
		socketFiles[name] = socketFile
	}
	return socketFiles, nil
}
