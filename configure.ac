AC_PREREQ([2.69])
AC_INIT([snap-confine], [1.0.30], [snapcraft@lists.ubuntu.com])
AC_CONFIG_SRCDIR([src/main.c])
AC_CONFIG_HEADERS([config.h])
AC_USE_SYSTEM_EXTENSIONS
AM_INIT_AUTOMAKE([foreign])
AM_MAINTAINER_MODE([enable])

# Checks for programs.
AC_PROG_CC_C99
AC_PROG_CPP
AC_PROG_INSTALL
AC_PROG_MAKE_SET

AC_LANG([C])
# Checks for libraries.

# Checks for header files.
AC_CHECK_HEADERS([fcntl.h limits.h stdlib.h string.h sys/mount.h unistd.h])

# Checks for typedefs, structures, and compiler characteristics.
AC_CHECK_HEADER_STDBOOL
AC_TYPE_UID_T
AC_TYPE_MODE_T
AC_TYPE_PID_T
AC_TYPE_SIZE_T

# Checks for library functions.
AC_FUNC_CHOWN
AC_FUNC_ERROR_AT_LINE
AC_FUNC_FORK
AC_FUNC_STRNLEN
AC_CHECK_FUNCS([mkdir regcomp setenv strdup strerror])

# Allow to build without confinement by calling:
# ./configure --disable-confinement
# This makes it possible to run snaps in devmode on almost any host,
# regardless of the kernel version.
AC_ARG_ENABLE([confinement],
    AS_HELP_STRING([--disable-confinement], [Disable strict confinement]),
    [case "${enableval}" in
        yes) enable_confinement=yes ;;
        no)  enable_confinement=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --disable-confinement])
    esac], [enable_confinement=yes])
AM_CONDITIONAL([STRICT_CONFINEMENT], [test "x$enable_confinement" = "xyes"])

# Check for required external libraries when confinement is enabled.
AS_IF([test "x$enable_confinement" = "xyes"], [
    PKG_CHECK_MODULES([APPARMOR], [libapparmor])
    PKG_CHECK_MODULES([SECCOMP], [libseccomp])
    PKG_CHECK_MODULES([LIBUDEV], [libudev])
    PKG_CHECK_MODULES([UDEV], [udev])
    AC_DEFINE([STRICT_CONFINEMENT], [1],
        [Define if strict apparmor confinement is available])
], [
    AC_MSG_WARN([
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    X                                                     X
    X Confinement disabled, all snaps will run in devmode X
    X                                                     X
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX])
])

# Allow to use the new execution environment based on pivot_root and bind-mount
# of the core snap. This is closer to how an all-snap system would execute
# applications (the root file system is a read only core snap) and is more
# flexible in how the host OS can look like (it can have any directory layout)
# and lastly it is much easier to implement arbitrary bind mounts from the
# classic environment.
AC_ARG_ENABLE([rootfs_is_core_snap],
    AS_HELP_STRING([--enable-rootfs-is-core-snap], [Use core snap as the root file system]),
    [case "${enableval}" in
        yes) enable_rootfs_is_core_snap=yes ;;
        no)  enable_rootfs_is_core_snap=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-rootfs-is-core-snap])
    esac], [enable_rootfs_is_core_snap=no])
AM_CONDITIONAL([ROOTFS_IS_CORE_SNAP], [test "x$enable_rootfs_is_core_snap" = "xyes"])

AS_IF([test "x$enable_rootfs_is_core_snap" = "xyes"], [
    AC_DEFINE([ROOTFS_IS_CORE_SNAP], [1],
        [Use the core snap as the root file system])])

# Enable special support for hosts with proprietary nvidia drivers on Ubuntu.
AC_ARG_ENABLE([nvidia-ubuntu],
    AS_HELP_STRING([--enable-nvidia-ubuntu], [Support for proprietary nvidia drivers (Ubuntu)]),
    [case "${enableval}" in
        yes) enable_nvidia_ubuntu=yes ;;
        no)  enable_nvidia_ubuntu=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-ubuntu])
    esac], [enable_nvidia_ubuntu=no])
AM_CONDITIONAL([NVIDIA_UBUNTU], [test "x$enable_nvidia_ubuntu" = "xyes"])

AS_IF([test "x$enable_nvidia_ubuntu" = "xyes"], [
    AC_DEFINE([NVIDIA_UBUNTU], [1],
        [Support for propietary nvidia drivers (Ubuntu)])])

# Enable special support for hosts with proprietary nvidia drivers on Arch.
AC_ARG_ENABLE([nvidia-arch],
    AS_HELP_STRING([--enable-nvidia-arch], [Support for proprietary nvidia drivers (Arch)]),
    [case "${enableval}" in
        yes) enable_nvidia_arch=yes ;;
        no)  enable_nvidia_arch=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-arch])
    esac], [enable_nvidia_arch=no])
AM_CONDITIONAL([NVIDIA_ARCH], [test "x$enable_nvidia_arch" = "xyes"])

AS_IF([test "x$enable_nvidia_arch" = "xyes"], [
    AC_DEFINE([NVIDIA_ARCH], [1],
        [Support for propietary nvidia drivers (Arch)])])

AC_CONFIG_FILES([Makefile src/Makefile tests/Makefile])
AC_OUTPUT
