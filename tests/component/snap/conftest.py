import os
import os.path
import sys
import subprocess

from subprocess import Popen
from time import sleep

import dbus
import dbus.mainloop.glib
import dbusmock
import pytest

dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)


@pytest.fixture(scope="function")
def dbus_monitor(request, dbus_session_bus):
    """Add this fixture to your test to debug it"""
    monitor = Popen(['/usr/bin/dbus-monitor', '--session'], stdout=sys.stdout)
    sleep(1)

    def teardown():
        monitor.terminate()
        monitor.wait()
    request.addfinalizer(teardown)


@pytest.fixture(scope="function")
def empty_root_dir(request, tmp_path):
    """Returns the global root dir to be injected into the snap process

    The directory is completely empty.
    """
    global_root_dir = tmp_path / 'root'
    global_root_dir.mkdir()
    return global_root_dir


@pytest.fixture(scope="function")
def root_dir(request, empty_root_dir):
    """Returns the global root dir to be injected into the snap process

    The directory is populated with the essential subdirectories normally
    found in a system.
    """
    global_root_dir = empty_root_dir
    xdg_runtime_dir = global_root_dir / 'run' / 'user' / str(os.getuid())
    xdg_runtime_dir.mkdir(parents=True)
    return global_root_dir


@pytest.fixture(scope="session")
def dbus_session_bus(request):
    """ Create a new D-Bus session bus

    The bus is returned, and can be used to interface with the services running
    in it. The environment is altered so that newly started processes will
    connect to this bus, and not to the one which might be running in the
    session of the user running the test suite.
    """
    start_dbus_daemon_command = [
        "dbus-daemon",
        "--session",
        "--nofork",
        "--print-address"
    ]

    dbus_daemon = Popen(
        start_dbus_daemon_command,
        shell=False,
        stdout=subprocess.PIPE)

    # Read the path of the newly created socket from STDOUT of
    # dbus-daemon, and set up the environment
    line = dbus_daemon.stdout.readline().decode('utf8')
    dbus_address = line.strip().split(",")[0]
    os.environ["DBUS_SESSION_BUS_ADDRESS"] = dbus_address
    bus = dbus.SessionBus()

    def teardown():
        bus.close()

        dbus_daemon.kill()
        dbus_daemon.wait()

    request.addfinalizer(teardown)
    return bus


@pytest.fixture(scope="class")
def snap_userd(request, make_command, dbus_session_bus, tmp_path_factory):
    """ Spawn a new `snap userd` service
    """
    root_dir = tmp_path_factory.mktemp("root")
    xdg_runtime_dir = root_dir / 'run' / 'user' / str(os.getuid())
    xdg_runtime_dir.mkdir(parents=True)

    environment = os.environ.copy()
    environment['SNAPPY_GLOBAL_ROOT'] = root_dir

    args = make_command('snap', 'userd', '--agent')

    # Spawn the service, and wait for it to open its socket
    service = Popen(
        args,
        stdout=sys.stdout,
        stderr=sys.stderr,
        env=environment)

    import socket
    xdg_runtime_dir = root_dir / 'run' / 'user' / str(os.getuid())
    socket_path = xdg_runtime_dir / 'snapd-session-agent.socket'
    snap_socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    socket_is_open = False
    for i in range(0, 50):
        try:
            snap_socket.connect(str(socket_path))
        except FileNotFoundError:
            sleep(0.1)
        else:
            snap_socket.close()
            socket_is_open = True
            break

    assert socket_is_open
    assert service.poll() is None, "service is not running"

    def teardown():
        service.terminate()
        service.wait()
    request.addfinalizer(teardown)

    return (service, str(socket_path))


@pytest.fixture(scope="function")
def request_name(request, service_name):
    """ Occupy the given service bus name

    This is intended as a helper to test how the service behaves when its
    bus name is already taken.
    """

    bus = dbus.SessionBus()

    def teardown():
        # Make sure the name is no longer there
        bus.release_name(service_name)
    request.addfinalizer(teardown)

    bus.request_name(service_name, dbus.bus.NAME_FLAG_REPLACE_EXISTING)
