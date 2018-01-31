AC_PREREQ([2.69])
AC_INIT([snap-data], m4_esyscmd_s([cat ../cmd/VERSION]), [snapcraft@lists.ubuntu.com])
AM_INIT_AUTOMAKE([foreign subdir-objects])
AM_MAINTAINER_MODE([enable])

# Checks for programs.
AC_PROG_INSTALL

SNAP_MOUNT_DIR="/snap"
AC_ARG_WITH([snap-mount-dir],
  AS_HELP_STRING([--with-snap-mount-dir=DIR], [Use an alternate snap mount directory]),
  [SNAP_MOUNT_DIR="$withval"])
AC_SUBST(SNAP_MOUNT_DIR)
AC_DEFINE_UNQUOTED([SNAP_MOUNT_DIR], "${SNAP_MOUNT_DIR}", [Location of the snap mount points])

AC_CONFIG_FILES([Makefile
                 systemd/Makefile
                 env/Makefile])
AC_OUTPUT

