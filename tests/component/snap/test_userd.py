import os
import re
import socket
import subprocess
import sys

from gi.repository import GLib
from time import sleep

import dbus
import pytest


class TestUserdStartup:

    def test_no_dbus(self, make_command, empty_root_dir):
        environment = os.environ.copy()
        environment["DBUS_SESSION_BUS_ADDRESS"] = ""
        environment["DBUS_SYSTEM_BUS_ADDRESS"] = ""
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            make_command('snap', 'userd'),
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'cannot find session bus' in output
        assert service.returncode == 1

    @pytest.mark.parametrize('service_name', [
        ('io.snapcraft.Launcher'),
        ('io.snapcraft.Settings'),
    ])
    def test_steal_name(self, make_command, dbus_session_bus, service_name,
                        request_name):
        args = make_command('snap', 'userd')
        service = subprocess.Popen(args, stderr=subprocess.PIPE)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'cannot obtain bus name' in output
        assert service_name in output
        assert service.returncode == 1


class TestUserdSessionAgent:

    def test_no_dbus(self, make_command, empty_root_dir):
        environment = os.environ.copy()
        environment["DBUS_SESSION_BUS_ADDRESS"] = ""
        environment["DBUS_SYSTEM_BUS_ADDRESS"] = ""
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            make_command('snap', 'userd', '--agent'),
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'Could not connect to session bus: cannot find session bus' \
            in output
        assert service.returncode == 1

    @pytest.mark.parametrize('service_name', [
        ('io.snapcraft.SessionAgent'),
    ])
    def test_steal_name(self, make_command, root_dir, dbus_session_bus,
                        service_name, request_name):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = root_dir
        service = subprocess.Popen(
            make_command('snap', 'userd', '--agent'),
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'cannot obtain bus name "io.snapcraft.SessionAgent"' in output
        assert service_name in output
        assert service.returncode == 1

    def test_no_runtime_dir(self, make_command, empty_root_dir,
                            dbus_session_bus):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            make_command('snap', 'userd', '--agent'),
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'error: cannot listen on socket' in output
        assert 'bind: no such file or directory' in output
        assert service.returncode == 1

    def test_socket_taken(self, make_command, root_dir, dbus_session_bus):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = root_dir

        # Occupy the socket
        xdg_runtime_dir = root_dir / 'run' / 'user' / str(os.getuid())
        socket_path = xdg_runtime_dir / 'snapd-session-agent.socket'
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        server.bind(str(socket_path))
        server.listen()

        service = subprocess.Popen(
            make_command('snap', 'userd', '--agent'),
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'error: cannot listen on socket' in output
        assert 'already in use' in output
        assert service.returncode == 1
        server.close()

    @pytest.mark.parametrize('request_body, expected_values', [
        # expected_values is a tuple
        # (app_name, summary, icon, body, urgency)
        pytest.param(
            '{"instance-name": "test-snap"}',
            ('', '', 'Snap "test-snap" is refreshing now!', '', 2)),
        pytest.param(
            # no idea how Go unmarshals time.Duration
            '{"time-remaining": 68000111000111, "instance-name": "snap1"}',
            ('', '',
             'Pending update of "snap1" snap',
             'Close the app to avoid disruptions (18 hours left)',
             1)
        ),
        pytest.param(
            '{"time-remaining": 98000111000111, "instance-name": "snap1"}',
            ('', '',
             'Pending update of "snap1" snap',
             'Close the app to avoid disruptions (1 day left)',
             0)
        ),
        pytest.param(
            '{"busy-app-name": "app2", "instance-name": "snap2" }',
            ('app2', '', 'Snap "snap2" is refreshing now!', '', 2)),
    ])
    def test_send_notification(self, snap_userd, fdo_notifications,
                               request_body, expected_values, dbus_monitor):
        socket_path = snap_userd[1]
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.connect(socket_path)
        request_data = ('POST /v1/notifications/pending-refresh HTTP/1.1\n' +
                        'Host: localhost\n' +
                        'Content-Length: {}\n'.format(len(request_body)) +
                        'Content-Type: application/json\n\n' +
                        request_body)
        s.send(request_data.encode('utf8'))
        reply = s.recv(10000)
        assert b'HTTP/1.1 200 OK' in reply

        calls = fdo_notifications.GetMethodCalls('Notify')
        assert len(calls) == 1
        params = calls[0][1]
        assert params[0] == expected_values[0]  # app_name
        assert params[1] == 0                   # replaces_id
        assert params[2] == expected_values[1]  # icon
        assert params[3] == expected_values[2]  # summary
        assert params[4] == expected_values[3]  # body
        assert len(params[5]) == 0              # actions
        assert params[6].get('desktop-entry') == 'io.snapcraft.SessionAgent'
        assert params[6].get('urgency') == expected_values[4]
        assert params[7] == 0                   # expire timeout


class TestUserd:
    @pytest.mark.parametrize('request_data, expected_error', [
        pytest.param(
            b'just some random string\n\n',
            b'HTTP/1.1 400 Bad Request'),
        pytest.param(
            b'GET /v1/session-info HTTP/1.1\n\n',
            b'HTTP/1.1 400 Bad Request: missing required Host header'),
        pytest.param(
            b'GET /v1/inexistent HTTP/1.1\nHost: me\n\n',
            b'HTTP/1.1 404 Not Found'),
    ])
    def test_invalid_requests(self, snap_userd, request_data,
                              expected_error):
        socket_path = snap_userd[1]
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.connect(socket_path)
        s.send(request_data)
        reply = s.recv(10000)
        eol = reply.index(b'\r\n')
        first_line = reply[:eol]
        assert first_line == expected_error
