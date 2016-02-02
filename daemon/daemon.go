// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

/*
#cgo pkg-config: libsystemd
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <systemd/sd-bus.h>

// Yes, this is as hacky as it looks. This copies the same method from libpolkit
static uint64_t
get_start_time (pid_t pid)
{
    char filename[1024], line[1024];
    FILE *f = NULL;
    int i, n_spaces;
    uint64_t start_time = 0;

    snprintf (filename, 1024, "/proc/%u/stat", pid);
    f = fopen (filename, "r");
    if (!fgets (line, 1024, f))
        goto done;

    // A line looks like this (the 22nd field is the start time - 545930 in this case):
    // 14207 (gnome-terminal-) S 3399 3741 3741 0 -1 4194304 95756 7917395 23 857 6850 942 29913 2486 20 0 4 0 545930 692899840 12642 18446744073709551615 4194304 4486204 140735647896976 140735647896432 139643272960061 0 0 4096 65536 0 0 0 17 3 0 0 1 0 0 6585664 6596840 22155264 140735647901892 140735647901938 140735647901938 140735647903690 0

    // Find the end of the name field, search from the right in case the name contains a ')'.
    for (i = strlen (line) - 1; i > 0 && line[i] != ')'; i--);
    if (line[i] != ')')
        goto done;

    // Skip 20 spaces to find the field that contains the start time
    for (n_spaces = 0; line[i] && n_spaces < 20; i++) {
        if (line[i] == ' ')
            n_spaces++;
    }
    if (line[i] == '\0')
        goto done;

    start_time = strtoull (line + i, NULL, 10);

done:
    if (f)
        fclose (f);

    return start_time;
}

int CheckAuthorization (const char *action_id, pid_t pid, uid_t uid)
{
    sd_bus *bus = NULL;
    sd_bus_error e = SD_BUS_ERROR_NULL;
    sd_bus_message *reply = NULL;
    int result;
    int authorized = false, challenge = false;

    if (sd_bus_open_system (&bus) < 0)
        goto done;

    result = sd_bus_call_method (bus,
                                 "org.freedesktop.PolicyKit1",
                                 "/org/freedesktop/PolicyKit1/Authority",
                                 "org.freedesktop.PolicyKit1.Authority",
                                 "CheckAuthorization",
                                 &e,
                                 &reply,
                                 "(sa{sv})sa{ss}us",
                                 "unix-process", 3, "pid", "u", pid, "start-time", "t", get_start_time (pid), "uid", "i", uid, // Subject
                                 action_id,
                                 0, // No details
                                 1, // 1 = allow user interaction
                                 ""); // Empty cancellation ID
    if (result < 0) {
        // Would be nice to report this error somewhere
        sd_bus_error_free (&e);
        goto done;
    }

    result = sd_bus_message_enter_container (reply, 'r', "bba{ss}");
    if (result < 0)
        goto done;

    result = sd_bus_message_read (reply, "bb", &authorized, &challenge);
    if (result < 0)
        goto done;

done:
    if (reply)
        sd_bus_message_unref (reply);
    if (bus) {
        sd_bus_close (bus);
        sd_bus_unref (bus);
    }

    return authorized;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/caps"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
)

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	sync.RWMutex // for concurrent access to the tasks map
	tasks        map[string]*Task
	listener     net.Listener
	tomb         tomb.Tomb
	router       *mux.Router
	capRepo      *caps.Repository
	asserts      *asserts.Database
}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request) Response

// A Command routes a request to an individual per-verb ResponseFUnc
type Command struct {
	Path string
	//
	GET    ResponseFunc
	PUT    ResponseFunc
	POST   ResponseFunc
	DELETE ResponseFunc
	// can guest GET?
	GuestOK bool
	// can non-admin GET?
	UserOK bool
	// can we modify with PolicyKit authorization?
	PolicyKitAction string
	//
	d *Daemon
}

func (c *Command) canAccess(r *http.Request) bool {
	isUser := false
	pid, uid, err := ucrednetGet(r.RemoteAddr)
	if err == nil {
		if uid == 0 {
			// superuser does anything
			return true
		}
		isUser = true
	}

	logger.Debugf("canAccess %s %d %d '%s'", r.Method, pid, uid, c.PolicyKitAction)

	// require authorization to modify
	if r.Method != "GET" {
		if isUser && c.PolicyKitAction != "" {
			return C.CheckAuthorization(C.CString(c.PolicyKitAction), C.pid_t(pid), C.uid_t(uid)) != C.false
		}
		return false
	}

	if isUser && c.UserOK {
		return true
	}

	if c.GuestOK {
		return true
	}

	return false
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.canAccess(r) {
		Forbidden("access denied").ServeHTTP(w, r)
		return
	}

	var rspf ResponseFunc
	var rsp = BadMethod("method %q not allowed", r.Method)

	switch r.Method {
	case "GET":
		rspf = c.GET
	case "PUT":
		rspf = c.PUT
	case "POST":
		rspf = c.POST
	case "DELETE":
		rspf = c.DELETE
	}

	if rspf != nil {
		rsp = rspf(c, r)
	}

	rsp.ServeHTTP(w, r)
}

type wrappedWriter struct {
	w http.ResponseWriter
	s int
}

func (w *wrappedWriter) Header() http.Header {
	return w.w.Header()
}

func (w *wrappedWriter) Write(bs []byte) (int, error) {
	return w.w.Write(bs)
}

func (w *wrappedWriter) WriteHeader(s int) {
	w.w.WriteHeader(s)
	w.s = s
}

func logit(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &wrappedWriter{w: w}
		t0 := time.Now()
		handler.ServeHTTP(ww, r)
		t := time.Now().Sub(t0)
		logger.Debugf("%s %s %s %s %d", r.RemoteAddr, r.Method, r.URL, t, ww.s)
	})
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	t0 := time.Now()
	listeners, err := activation.Listeners(false)
	if err != nil {
		return err
	}

	if len(listeners) != 1 {
		return fmt.Errorf("daemon does not handle %d listeners right now, just one", len(listeners))
	}

	d.listener = &ucrednetListener{listeners[0]}

	d.addRoutes()

	logger.Debugf("init done in %s", time.Now().Sub(t0))

	return nil
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		logger.Debugf("adding %s", c.Path)
		d.router.Handle(c.Path, c).Name(c.Path)
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = NotFound("not found")
}

// Start the Daemon
func (d *Daemon) Start() {
	d.tomb.Go(func() error {
		return http.Serve(d.listener, logit(d.router))
	})
}

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)
	return d.tomb.Wait()
}

// Dying is a tomb-ish thing
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}

// AddTask runs the given function as a task
func (d *Daemon) AddTask(f func() interface{}) *Task {
	t := RunTask(f)
	d.Lock()
	defer d.Unlock()
	d.tasks[t.UUID()] = t

	return t
}

// GetTask retrieves a task from the tasks map, by uuid.
func (d *Daemon) GetTask(uuid string) *Task {
	d.RLock()
	defer d.RUnlock()
	return d.tasks[uuid]
}

var (
	errTaskNotFound     = errors.New("task not found")
	errTaskStillRunning = errors.New("task still running")
)

// DeleteTask removes a task from the tasks map, by uuid.
func (d *Daemon) DeleteTask(uuid string) error {
	d.Lock()
	defer d.Unlock()
	task, ok := d.tasks[uuid]
	if !ok || task == nil {
		return errTaskNotFound
	}
	if task.State() != TaskRunning {
		delete(d.tasks, uuid)
		return nil
	}

	return errTaskStillRunning
}

func getTrustedAccountKey() string {
	if !helpers.FileExists(dirs.SnapTrustedAccountKey) {
		// XXX: allow this fallback here for integration tests,
		// until we have a proper trusted public key shared
		// with the store
		return os.Getenv("SNAPPY_TRUSTED_ACCOUNT_KEY")
	}
	return dirs.SnapTrustedAccountKey
}

// New Daemon
func New() *Daemon {
	db, err := asserts.OpenSysDatabase(getTrustedAccountKey())
	if err != nil {
		panic(err.Error())
	}
	repo := caps.NewRepository()
	err = caps.LoadBuiltInTypes(repo)
	if err != nil {
		panic(err.Error())
	}
	return &Daemon{
		tasks:   make(map[string]*Task),
		capRepo: repo,
		asserts: db,
	}
}
