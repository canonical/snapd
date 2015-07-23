// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"path/filepath"
	"time"

	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/systemd"
)

type svcT struct {
	m   *packageYaml
	svc *ServiceYaml
}

type ServiceActor struct {
	svcs []*svcT
	pb   progress.Meter
	sysd systemd.Systemd
}

// FindServices finds all matching services (empty string matches all)
// and lets you perform different actions (start, stop, etc) on them.
func FindServices(snapName string, serviceName string, pb progress.Meter) (*ServiceActor, error) {
	var svcs []*svcT

	repo := NewMetaLocalRepository()
	installed, _ := repo.Installed()

	foundSnap := false
	for _, part := range installed {
		snap, ok := part.(*SnapPart)
		if !ok {
			// can't happen
			continue
		}
		if snapName != "" && snapName != snap.Name() {
			continue
		}
		foundSnap = true

		yamls := snap.ServiceYamls()
		for i := range yamls {
			if serviceName != "" && serviceName != yamls[i].Name {
				continue
			}
			s := &svcT{
				m:   snap.m,
				svc: &yamls[i],
			}
			svcs = append(svcs, s)
		}
	}
	if !foundSnap {
		return nil, ErrPackageNotFound
	}
	if len(svcs) == 0 {
		return nil, ErrServiceNotFound
	}

	return &ServiceActor{
		svcs: svcs,
		pb:   pb,
		sysd: systemd.New(globalRootDir, pb),
	}, nil
}

func (actor *ServiceActor) Status() ([]string, error) {
	var stati []string
	for _, svc := range actor.svcs {
		svcname := filepath.Base(generateServiceFileName(svc.m, *svc.svc))
		status, err := actor.sysd.Status(svcname)
		if err != nil {
			return nil, err
		}
		status = fmt.Sprintf("%s\t%s\t%s", svc.m.Name, svc.svc.Name, status)
		stati = append(stati, status)
	}

	return stati, nil
}

func (actor *ServiceActor) Start() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(generateServiceFileName(svc.m, *svc.svc))
		if err := actor.sysd.Start(svcname); err != nil {
			return fmt.Errorf(i18n.G("unable to start %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	return nil
}

func (actor *ServiceActor) Stop() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(generateServiceFileName(svc.m, *svc.svc))
		if err := actor.sysd.Stop(svcname, time.Duration(svc.svc.StopTimeout)); err != nil {
			return fmt.Errorf(i18n.G("unable to stop %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	return nil
}

func (actor *ServiceActor) Restart() error {
	err := actor.Stop()
	if err != nil {
		return err
	}

	return actor.Start()
}
