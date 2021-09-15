import os
import re
import subprocess
import sys

from gi.repository import GLib
from time import sleep

import dbus
import pytest


class TestUserdStartup:

    def test_no_dbus(self, snap_command, empty_root_dir):
        environment = os.environ.copy()
        environment["DBUS_SESSION_BUS_ADDRESS"] = ""
        environment["DBUS_SYSTEM_BUS_ADDRESS"] = ""
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            snap_command + ['userd'],
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
    def test_steal_name(self, snap_command, dbus_session_bus, service_name,
                        request_name):
        args = snap_command + ['userd']
        service = subprocess.Popen(args, stderr=subprocess.PIPE)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'cannot obtain bus name' in output
        assert service_name in output
        assert service.returncode == 1


class TestUserdSessionAgent:

    def test_no_dbus(self, snap_command, empty_root_dir):
        environment = os.environ.copy()
        environment["DBUS_SESSION_BUS_ADDRESS"] = ""
        environment["DBUS_SYSTEM_BUS_ADDRESS"] = ""
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            snap_command + ['userd', '--agent'],
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
    def test_steal_name(self, snap_command, root_dir, dbus_session_bus,
                        service_name, request_name):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = root_dir
        service = subprocess.Popen(
            snap_command + ['userd', '--agent'],
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'cannot obtain bus name "io.snapcraft.SessionAgent"' in output
        assert service_name in output
        assert service.returncode == 1

    def test_no_runtime_dir(self, snap_command, empty_root_dir,
                            dbus_session_bus):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = empty_root_dir
        service = subprocess.Popen(
            snap_command + ['userd', '--agent'],
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'error: cannot listen on socket' in output
        assert 'bind: no such file or directory' in output
        assert service.returncode == 1

    def test_socket_taken(self, snap_command, root_dir, dbus_session_bus):
        environment = os.environ.copy()
        environment['SNAPPY_GLOBAL_ROOT'] = root_dir

        # Occupy the socket
        import socket
        xdg_runtime_dir = root_dir / 'run' / 'user' / str(os.getuid())
        socket_path = xdg_runtime_dir / 'snapd-session-agent.socket'
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        server.bind(str(socket_path))
        server.listen()

        service = subprocess.Popen(
            snap_command + ['userd', '--agent'],
            stderr=subprocess.PIPE,
            env=environment)
        service.wait(5)
        output = str(service.stderr.read())
        assert 'error: cannot listen on socket' in output
        assert 'already in use' in output
        assert service.returncode == 1
        server.close()
