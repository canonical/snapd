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
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
)

// SnapServicesUnitOptions is a struct for controlling the generated service
// definition for a snap service.
type SnapServicesUnitOptions struct {
	// VitalityRank is the rank of all services in the specified snap used by
	// the OOM killer when OOM conditions are reached.
	VitalityRank int

	// QuotaGroup is the quota group for the service.
	QuotaGroup *quota.Group

	// CoreMountedSnapdSnapDep is whether the generated unit should depend on
	// the provided snapd snapd being mounted
	CoreMountedSnapdSnapDep string
}

func serviceStopTimeout(app *snap.AppInfo) time.Duration {
	tout := app.StopTimeout
	if tout == 0 {
		tout = timeout.DefaultTimeout
	}
	return time.Duration(tout)
}

func generateServiceNames(snap *snap.Info, appNames []string) []string {
	names := make([]string, 0, len(appNames))

	for _, name := range appNames {
		if app := snap.Apps[name]; app != nil {
			names = append(names, app.ServiceName())
		}
	}
	return names
}

func GenerateSnapServiceUnitFile(appInfo *snap.AppInfo, opts *SnapServicesUnitOptions) ([]byte, error) {
	if opts == nil {
		opts = &SnapServicesUnitOptions{}
	}

	if err := snap.ValidateApp(appInfo); err != nil {
		return nil, err
	}

	// assemble all of the service directive snippets for all interfaces that
	// this service needs to include in the generated systemd file

	// use an ordered set to ensure we don't duplicate any keys from interfaces
	// that specify the same snippet

	// TODO: maybe we should error if multiple interfaces specify different
	// values for the same directive, otherwise one of them will overwrite the
	// other? What happens right now is that the snippet from the plug that
	// comes last will win in the case of directives that can have only one
	// value, but for some directives, systemd combines their values into a
	// list.
	ifaceServiceSnippets := &strutil.OrderedSet{}

	for _, plug := range appInfo.Plugs {
		iface, err := interfaces.ByName(plug.Interface)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		snips, err := interfaces.PermanentPlugServiceSnippets(iface, plug)
		if err != nil {
			return nil, fmt.Errorf("error processing plugs while generating service unit for %v: %v", appInfo.SecurityTag(), err)
		}
		for _, snip := range snips {
			ifaceServiceSnippets.Put(snip)
		}
	}

	// join the service snippets into one string to be included in the
	// template
	ifaceSpecifiedServiceSnippet := strings.Join(ifaceServiceSnippets.Items(), "\n")

	serviceTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit }}
Requires={{.MountUnit}}
{{- end }}
{{- if .PrerequisiteTarget}}
Wants={{.PrerequisiteTarget}}
{{- end}}
{{- if .After}}
After={{ stringsJoin .After " " }}
{{- end}}
{{- if .Before}}
Before={{ stringsJoin .Before " "}}
{{- end}}
{{- if .CoreMountedSnapdSnapDep}}
Wants={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
After={{ stringsJoin .CoreMountedSnapdSnapDep " "}}
{{- end}}
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart={{.App.LauncherCommand}}
SyslogIdentifier={{.App.Snap.InstanceName}}.{{.App.Name}}
Restart={{.Restart}}
{{- if .App.RestartDelay}}
RestartSec={{.App.RestartDelay.Seconds}}
{{- end}}
WorkingDirectory={{.WorkingDir}}
{{- if .App.StopCommand}}
ExecStop={{.App.LauncherStopCommand}}
{{- end}}
{{- if .App.ReloadCommand}}
ExecReload={{.App.LauncherReloadCommand}}
{{- end}}
{{- if .App.PostStopCommand}}
ExecStopPost={{.App.LauncherPostStopCommand}}
{{- end}}
{{- if .StopTimeout}}
TimeoutStopSec={{.StopTimeout.Seconds}}
{{- end}}
{{- if .StartTimeout}}
TimeoutStartSec={{.StartTimeout.Seconds}}
{{- end}}
Type={{.App.Daemon}}
{{- if .Remain}}
RemainAfterExit={{.Remain}}
{{- end}}
{{- if .BusName}}
BusName={{.BusName}}
{{- end}}
{{- if .App.WatchdogTimeout}}
WatchdogSec={{.App.WatchdogTimeout.Seconds}}
{{- end}}
{{- if .KillMode}}
KillMode={{.KillMode}}
{{- end}}
{{- if .KillSignal}}
KillSignal={{.KillSignal}}
{{- end}}
{{- if .OOMAdjustScore }}
OOMScoreAdjust={{.OOMAdjustScore}}
{{- end}}
{{- if .InterfaceServiceSnippets}}
{{.InterfaceServiceSnippets}}
{{- end}}
{{- if .SliceUnit}}
Slice={{.SliceUnit}}
{{- end}}
{{- if .LogNamespace}}
LogNamespace={{.LogNamespace}}
{{- end}}
{{- if not (or .App.Sockets .App.Timer .App.ActivatesOn) }}

[Install]
WantedBy={{.ServicesTarget}}
{{- end}}
`
	var templateOut bytes.Buffer
	tmpl := template.New("service-wrapper")
	tmpl.Funcs(template.FuncMap{
		"stringsJoin": strings.Join,
	})
	t := template.Must(tmpl.Parse(serviceTemplate))

	restartCond := appInfo.RestartCond.String()
	if restartCond == "" {
		restartCond = snap.RestartOnFailure.String()
	}

	// use score -900+vitalityRank, where vitalityRank starts at 1
	// and considering snapd itself has OOMScoreAdjust=-900
	const baseOOMAdjustScore = -900
	var oomAdjustScore int
	if opts.VitalityRank > 0 {
		oomAdjustScore = baseOOMAdjustScore + opts.VitalityRank
	}

	var remain string
	if appInfo.Daemon == "oneshot" {
		// any restart condition other than "no" is invalid for oneshot daemons
		restartCond = "no"
		// If StopExec is present for a oneshot service than we also need
		// RemainAfterExit=yes
		if appInfo.StopCommand != "" {
			remain = "yes"
		}
	}
	var killMode string
	if !appInfo.StopMode.KillAll() {
		killMode = "process"
	}

	var busName string
	if appInfo.Daemon == "dbus" {
		busName = appInfo.BusName
		if busName == "" && len(appInfo.ActivatesOn) != 0 {
			slot := appInfo.ActivatesOn[len(appInfo.ActivatesOn)-1]
			if err := slot.Attr("name", &busName); err != nil {
				// This should be impossible for a valid AppInfo
				logger.Noticef("Cannot get 'name' attribute of dbus slot %q: %v", slot.Name, err)
			}
		}
	}

	wrapperData := struct {
		App *snap.AppInfo

		Restart                  string
		WorkingDir               string
		StopTimeout              time.Duration
		StartTimeout             time.Duration
		ServicesTarget           string
		PrerequisiteTarget       string
		MountUnit                string
		Remain                   string
		KillMode                 string
		KillSignal               string
		OOMAdjustScore           int
		BusName                  string
		Before                   []string
		After                    []string
		InterfaceServiceSnippets string
		SliceUnit                string
		LogNamespace             string

		Home    string
		EnvVars string

		CoreMountedSnapdSnapDep []string
	}{
		App: appInfo,

		InterfaceServiceSnippets: ifaceSpecifiedServiceSnippet,

		Restart:        restartCond,
		StopTimeout:    serviceStopTimeout(appInfo),
		StartTimeout:   time.Duration(appInfo.StartTimeout),
		Remain:         remain,
		KillMode:       killMode,
		KillSignal:     appInfo.StopMode.KillSignal(),
		OOMAdjustScore: oomAdjustScore,
		BusName:        busName,

		Before: generateServiceNames(appInfo.Snap, appInfo.Before),
		After:  generateServiceNames(appInfo.Snap, appInfo.After),

		// systemd runs as PID 1 so %h will not work.
		Home: "/root",
	}
	switch appInfo.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.ServicesTarget = systemd.ServicesTarget
		wrapperData.PrerequisiteTarget = systemd.PrerequisiteTarget
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir()))
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
		wrapperData.After = append(wrapperData.After, "snapd.apparmor.service")
	case snap.UserDaemon:
		wrapperData.ServicesTarget = systemd.UserServicesTarget
		// FIXME: ideally use UserDataDir("%h"), but then the
		// unit fails if the directory doesn't exist.
		wrapperData.WorkingDir = appInfo.Snap.DataDir()
	default:
		panic("unknown snap.DaemonScope")
	}

	// check the quota group slice
	if opts.QuotaGroup != nil {
		wrapperData.SliceUnit = opts.QuotaGroup.SliceFileName()
		if opts.QuotaGroup.JournalQuotaSet() {
			wrapperData.LogNamespace = opts.QuotaGroup.JournalNamespaceName()
		}
	}

	// Add extra "After" targets
	if wrapperData.PrerequisiteTarget != "" {
		wrapperData.After = append([]string{wrapperData.PrerequisiteTarget}, wrapperData.After...)
	}
	if wrapperData.MountUnit != "" {
		wrapperData.After = append([]string{wrapperData.MountUnit}, wrapperData.After...)
	}
	if opts.CoreMountedSnapdSnapDep != "" {
		wrapperData.CoreMountedSnapdSnapDep = []string{opts.CoreMountedSnapdSnapDep}
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}
