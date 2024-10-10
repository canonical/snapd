# This makefiles uses grouped-target feature and relies on it for correctness.
ifeq (,$(findstring grouped-target,$(.FEATURES)))
$(error You need make with the grouped-taget feature to build this dataset)
endif

fstab_files = content-layout-1-initially-connected.current.fstab \
			  content-layout-1-initially-connected.desired.fstab \
			  content-layout-2-after-disconnect.desired.fstab \
			  content-layout-2-after-disconnect.current.fstab \
			  content-layout-3-after-reconnect.desired.fstab \
			  content-layout-3-after-reconnect.current.fstab \
			  content-layout-4-initially-disconnected-then-connected.before.current.fstab \
			  content-layout-4-initially-disconnected-then-connected.desired.fstab \
			  content-layout-4-initially-disconnected-then-connected.current.fstab \
			  content-layout-5-initially-connected-then-content-refreshed.before.current.fstab \
			  content-layout-5-initially-connected-then-content-refreshed.desired.fstab \
			  content-layout-5-initially-connected-then-content-refreshed.current.fstab \
			  content-layout-6-initially-connected-then-app-refreshed.before.current.fstab \
			  content-layout-6-initially-connected-then-app-refreshed.desired.fstab \
			  content-layout-6-initially-connected-then-app-refreshed.current.fstab

# None of the fstab files can be built in parallel as the depend on global system state.
.NOTPARALLEL: $(fstab_files)
.PHONY: all
all: $(fstab_files)

content-layout-1-initially-connected.desired.fstab content-layout-1-initially-connected.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	snap run test-snapd-content-layout.sh -c true
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-1-initially-connected.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-1-initially-connected.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-1.patch

content-layout-2-after-disconnect.desired.fstab content-layout-2-after-disconnect.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	snap run test-snapd-content-layout.sh -c true
	sudo snap disconnect test-snapd-content-layout:just-content test-snapd-content:just-content
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-2-after-disconnect.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-2-after-disconnect.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-2.patch

content-layout-3-after-reconnect.desired.fstab content-layout-3-after-reconnect.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	snap run test-snapd-content-layout.sh -c true
	sudo snap disconnect test-snapd-content-layout:just-content test-snapd-content:just-content
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-3-after-reconnect.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-3-after-reconnect.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-3.patch

content-layout-4-initially-disconnected-then-connected.before.current.fstab content-layout-4-initially-disconnected-then-connected.desired.fstab content-layout-4-initially-disconnected-then-connected.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	snap connections test-snapd-content-layout | grep -xF 'content    test-snapd-content-layout:just-content  -     -'
	snap run test-snapd-content-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-4-initially-disconnected-then-connected.before.current.fstab
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-4-initially-disconnected-then-connected.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-4-initially-disconnected-then-connected.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-4.patch

content-layout-5-initially-connected-then-content-refreshed.before.current.fstab content-layout-5-initially-connected-then-content-refreshed.desired.fstab content-layout-5-initially-connected-then-content-refreshed.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	snap run test-snapd-content-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-5-initially-connected-then-content-refreshed.before.current.fstab
	sudo snap install --dangerous $(word 2,$^) # Refresh the content.
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-5-initially-connected-then-content-refreshed.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-5-initially-connected-then-content-refreshed.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-5.patch

content-layout-6-initially-connected-then-app-refreshed.before.current.fstab content-layout-6-initially-connected-then-app-refreshed.desired.fstab content-layout-6-initially-connected-then-app-refreshed.current.fstab &: test-snapd-content-layout_a_all.snap test-snapd-content_a_all.snap
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	sudo snap install --dangerous $(word 1,$^)
	sudo snap install --dangerous $(word 1,$^) # Reinstall to get another revision for better clarity of the data.
	sudo snap install --dangerous $(word 2,$^)
	sudo snap connect test-snapd-content-layout:just-content test-snapd-content:just-content
	snap run test-snapd-content-layout.sh -c true
	# This file is used by unit tests model the changes needed after the refresh below.
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-6-initially-connected-then-app-refreshed.before.current.fstab
	sudo snap install --dangerous $(word 1,$^) # Refresh the app.
	cp /var/lib/snapd/mount/snap.test-snapd-content-layout.fstab content-layout-6-initially-connected-then-app-refreshed.desired.fstab
	cp /run/snapd/ns/snap.test-snapd-content-layout.fstab content-layout-6-initially-connected-then-app-refreshed.current.fstab
	sudo snap remove --purge test-snapd-content-layout
	sudo snap remove --purge test-snapd-content
	patch <annotations-6.patch

test-snapd-content-layout_a_all.snap: test-snapd-content-layout
	snap pack $<

test-snapd-content_a_all.snap: test-snapd-content
	snap pack $<
