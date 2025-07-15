FROM fedora:42

RUN dnf makecache && \
    dnf -y --refresh install --setopt=install_weak_deps=False rpm-build rpmdevtools mock

RUN useradd mockbuilder && usermod -a -G mock mockbuilder

USER mockbuilder

# RUN mock -r /etc/mock/fedora-41-x86_64.cfg --install git
# RUN mock -r /etc/mock/fedora-42-x86_64.cfg --install git
# RUN mock -r /etc/mock/centos-stream-9-x86_64.cfg --install git
# RUN mock -r /etc/mock/opensuse-leap-15.6-x86_64.cfg --install git
# RUN mock -r /etc/mock/opensuse-tumbleweed-x86_64.cfg --install git
# RUN mock -r /etc/mock/amazonlinux-2-x86_64.cfg --install git
# RUN mock -r /etc/mock/amazonlinux-2023-x86_64.cfg --install git

# ARG SYSTEM
# COPY packaging/${SYSTEM}/snapd.spec snapd.spec

# RUN dnf -y --refresh install --setopt=install_weak_deps=False $(rpmspec -q --buildrequires snapd.spec)


# sudo docker run --privileged --cap-add=SYS_ADMIN --name mock --mount type=bind,src=/home/katie/source/snapd,dst=/home/mockbuilder/snapd --user mockbuilder -it mock-base
# sudo docker run --privileged --name mock --mount type=bind,src=/home/katie/source/snapd,dst=/home/mockbuilder/snapd --user mockbuilder -it mock-base


# mock -r /etc/mock/fedora-41-x86_64.cfg --install git
# mock -r /etc/mock/fedora-41-x86_64.cfg --no-clean --enable-network --nocheck --with testkeys /home/mockbuilder/rpmbuild/SRPMS/snapd-1337.2.70-0.fc42.src.rpm