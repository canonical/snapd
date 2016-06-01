AC_PREREQ([2.69])
AC_INIT([snap-confine], [2.0], [snapcraft@lists.ubuntu.com])
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

# Check for required external libraries
PKG_CHECK_MODULES([UDEV], [libudev])
PKG_CHECK_MODULES([SECCOMP], [libseccomp])

# Check for apparmor when confinement in enabled.
AS_IF([test "x$enable_confinement" = "xyes"], [
    PKG_CHECK_MODULES([APPARMOR], [libapparmor])
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

AC_CONFIG_FILES([Makefile src/Makefile tests/Makefile])
AC_OUTPUT
