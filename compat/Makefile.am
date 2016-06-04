dist_bin_SCRIPTS = ubuntu-core-launcher
CLEANFILES = $(dist_bin_SCRIPTS)

ubuntu-core-launcher: ubuntu-core-launcher.in
	sed -e 's,[@]LIBDIR[@],$(libdir),g' < $(srcdir)/ubuntu-core-launcher.in > ubuntu-core-launcher
	chmod +x ubuntu-core-launcher
