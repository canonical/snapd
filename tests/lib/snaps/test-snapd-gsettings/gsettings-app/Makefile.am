gsettings_SCHEMAS = com.canonical.test.snapd.gsettings.gschema.xml

@GSETTINGS_RULES@

# The actual runnable program is set to the SCRIPTS primitive.
# # Prefix bin_ tells where to copy this
bin_SCRIPTS = hello-world.py

# # List of files to be distributed
EXTRA_DIST=  \
	$(gsettings_SCHEMAS)

# # The desktop files
desktopdir = $(datadir)/applications
desktop_DATA = \
	hello-world.desktop

