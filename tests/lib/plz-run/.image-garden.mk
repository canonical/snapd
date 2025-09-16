# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Zygmunt Krynicki

define UBUNTU_CLOUD_INIT_USER_DATA_TEMPLATE
$(CLOUD_INIT_USER_DATA_TEMPLATE)
- snap install --classic go
endef

# Unorthodox classic confinement on core systems.
define UBUNTU_CORE_CLOUD_INIT_USER_DATA_TEMPLATE
$(BASE_UBUNTU_CORE_CLOUD_INIT_USER_DATA_TEMPLATE)
- snap download --target-directory=/tmp go
- mkdir /snap/go
- unsquashfs -d /snap/go/x1 /tmp/go_*.snap
- ln -s ../go/x1/bin/go /snap/bin/go
endef
