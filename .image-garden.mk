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

define OPENSUSE_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# Switch the primary LSM to AppArmor on openSUSE systems.
- sed -i -e 's/security=selinux/security=apparmor/g' /etc/default/grub
- update-bootloader
# Add empty /etc/environment file that snapd test suite wants to back up.
- touch /etc/environment
endef

define UBUNTU_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
endef

define UBUNTU_16.04_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
# The cloud image seems not to have ntp anymore but spread prepare stops the
# service so pre-install it before we make other changes. Perhaps the selection
# in server images is different.
packages:
- ntp
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
