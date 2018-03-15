AC_PREREQ([2.69])
AC_INIT([snap-data], m4_esyscmd_s([cat ../cmd/VERSION]), [snapcraft@lists.ubuntu.com])
AM_INIT_AUTOMAKE([foreign subdir-objects])
AM_MAINTAINER_MODE([enable])

# Checks for programs.
AC_PROG_INSTALL
PKG_PROG_PKG_CONFIG

SNAP_MOUNT_DIR="/snap"
AC_ARG_WITH([snap-mount-dir],
  AS_HELP_STRING([--with-snap-mount-dir=DIR], [Use an alternate snap mount directory]),
  [SNAP_MOUNT_DIR="$withval"])
AC_SUBST(SNAP_MOUNT_DIR)
AC_DEFINE_UNQUOTED([SNAP_MOUNT_DIR], "${SNAP_MOUNT_DIR}", [Location of the snap mount points])

SNAPD_ENVIRONMENT_FILE="/etc/environment"
AC_ARG_WITH([snapd-environment-file],
  AS_HELP_STRING([--with-snapd-environment-file=DIR], [Use this file to set-up environment of snapd.service]),
  [SNAPD_ENVIRONMENT_FILE="$withval"])
AC_SUBST(SNAPD_ENVIRONMENT_FILE)

SYSTEMD_SYSTEM_UNIT_DIR=$($PKG_CONFIG --variable=systemdsystemunitdir systemd)
AC_ARG_WITH([systemd-system-unit-dir],
  AS_HELP_STRING([--with-systemd-system-unit-dir=DIR], [Location of systemd system units directory]),
  [SYSTEMD_SYSTEM_UNIT_DIR="$withval"])
AC_SUBST(SYSTEMD_SYSTEM_UNIT_DIR)

DBUS_SESSION_SERVICES_DIR=$($PKG_CONFIG --variable=session_bus_services_dir dbus-1)
AC_ARG_WITH([dbus-session-services-dir],
AS_HELP_STRING([--with-dbus-session-services-dir=DIR], [Location of DBus session services directory]),
[DBUS_SESSION_SERVICES_DIR="$withval"])
AC_SUBST(DBUS_SESSION_SERVICES_DIR)

AC_CONFIG_FILES([Makefile
                 systemd/Makefile
                 env/Makefile
                 dbus/Makefile])
AC_OUTPUT

