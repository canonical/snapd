define AMAZONLINUX_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define ARCHLINUX_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# Pre-install apparmor on ArchLinux systems.
packages:
- apparmor
endef

define CENTOS_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define DEBIAN_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define FEDORA_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define OPENSUSE_tumbleweed_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# Switch the primary LSM to AppArmor on openSUSE systems.
- sed -i -e 's/security=selinux/security=apparmor/g' /etc/default/grub
- sed -i -e 's/selinux=1//g' /etc/default/grub
- sed -i -e 's/^SELINUX=enforcing/SELINUX=disabled/g' /etc/selinux/config
- update-bootloader
# Add empty /etc/environment file that snapd test suite wants to back up.
- touch /etc/environment
endef

define OPENSUSE_tumbleweed-selinux_CLOUD_INIT_USER_DATA_TEMPLATE
$(BASE_CLOUD_INIT_USER_DATA_TEMPLATE)
- sed -i -e 's/^SELINUX=enforcing/SELINUX=permissive/g' /etc/selinux/config
# Add empty /etc/environment file that snapd test suite wants to back up.
- touch /etc/environment
endef

define UBUNTU_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

# In the snapd project Ubuntu Core images are built from classic Ubuntu images
# in a somewhat complex manner. Ubuntu Core 16 and 18 kernels do not support
# booting from. Use a quirk to make those systems use SCSI storage instead.
# The quirk is taken directly from image-garden's identical quirk for
# ubuntu-core-16 and ubuntu-core-18 systems.
ubuntu-cloud-16.04.x86_64.qcow2 ubuntu-cloud-16.04.x86_64.run ubuntu-cloud-18.04.x86_64.qcow2 ubuntu-cloud-18.04.x86_64.run: QEMU_ENV_QUIRKS=export QEMU_STORAGE_OPTION="$(strip \
    -drive file=$(1),if=none,format=qcow2,id=drive0,media=disk,cache=writeback,discard=unmap \
    -device virtio-scsi-pci,id=scsi0 \
    -device scsi-hd,drive=drive0,bus=scsi0.0,bootindex=0)";

# XXX: small hack to offer two versions of openSUSE Tumbleweed.

opensuse-cloud-tumbleweed-selinux.x86_64.run: $(MAKEFILE_LIST) | opensuse-cloud-tumbleweed-selinux.x86_64.qcow2 opensuse-cloud-tumbleweed.x86_64.efi-code.img opensuse-cloud-tumbleweed.x86_64.efi-vars.img
	echo "#!/bin/sh" >$@
	echo 'set -xeu' >>$@
	echo "# WARNING: The .qcow2 file refers to a file in $(GARDEN_DL_DIR)/opensuse" >>$@
	echo '$(strip $(call QEMU_SYSTEM_X86_64_EFI_CMDLINE,$(word 1,$|),$(word 2,$|),$(word 3,$|)) "$$@")' >>$@
	chmod +x $@

opensuse-cloud-tumbleweed-selinux.x86_64.qcow2: $(GARDEN_DL_DIR)/opensuse/opensuse-cloud-tumbleweed.x86_64.qcow2 opensuse-cloud-tumbleweed-selinux.x86_64.seed.iso opensuse-cloud-tumbleweed-selinux.x86_64.efi-code.img opensuse-cloud-tumbleweed-selinux.x86_64.efi-vars.img
	$(strip $(QEMU_IMG) create -f qcow2 -b $< -F qcow2 $@ $(QEMU_IMG_SIZE))
	$(strip $(call QEMU_SYSTEM_X86_64_EFI_CMDLINE,$@,$(word 3,$^),$(word 4,$^)) \
    -drive file=$(word 2,$^),format=raw,id=drive1,if=none,readonly=true,media=cdrom \
    -device virtio-blk,drive=drive1 \
    | tee $@.log)

opensuse-cloud-tumbleweed-selinux.x86_64.meta-data: export META_DATA = $(call CLOUD_INIT_META_DATA_TEMPLATE,opensuse)
opensuse-cloud-tumbleweed-selinux.x86_64.meta-data: $(MAKEFILE_LIST)
	echo "$${META_DATA}" | tee $@
	touch --reference=$(shell stat $^ -c '%Y %n' | sort -nr | cut -d ' ' -f 2 | head -n 1) $@

opensuse-cloud-tumbleweed-selinux.x86_64.user-data: export USER_DATA = $(call $(call PICK_CLOUD_INIT_USER_DATA_TEMPLATE_FOR,OPENSUSE,tumbleweed-selinux),opensuse-tumbleweed,opensuse)
opensuse-cloud-tumbleweed-selinux.x86_64.user-data: $(MAKEFILE_LIST) $(wildcard $(GARDEN_PROJECT_DIR)/.image-garden.mk)
	echo "$${USER_DATA}" | tee $@
	touch --reference=$(shell stat $^ -c '%Y %n' | sort -nr | cut -d ' ' -f 2 | head -n 1) $@
