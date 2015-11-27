# Autoupdate

*Autoupdate* is a feature that will guarantee you are always up to
date. It is enabled by default, and can be disabled via `snappy config`.

## Usage

To check whether the feature is active, run

    snappy config ubuntu-core | grep autoupdate

If you want to disable it run

    echo 'config: {ubuntu-core: {autoupdate: off}}' | sudo snappy config ubuntu-core -

and you then re-enable it via

    echo 'config: {ubuntu-core: {autoupdate: on}}' | sudo snappy config ubuntu-core -

Every time autoupdate triggers it will try to update the whole system;
if an `ubuntu-core` update is available the system will automatically
reboot, although a message is printed to console with instructions on
how to abort the reboot, in case you are logged in at the time.

> If you need a single configuration that works both on 15.04 and rolling, you
> can use both the old and new keys, e.g. `config: {ubuntu-core: {autoupdate:
> on, autopilot: on}}`.

## Implementation details

Autoupdate used to be called *autopilot* (but that got very confusing,
especially when people were using snappy with other things that have
their own autopilot, like an OpenStack deployment that used
Canonical's own OpenStack Autopilot, or in mobile robots that could
fly themselves); the `systemd` units still use this name.

For more details of when it is to be triggered you could dig into the
implementation, via

    systemctl list-timers snappy-autopilot.timer

To check whether the update ran, run

    systemctl status -l snappy-autopilot.service

and to view any output from the command run

    sudo journalctl -u snappy-autopilot.service
