FROM fedora:42

RUN dnf makecache && \
    dnf -y --refresh install --setopt=install_weak_deps=False rpm-build rpmdevtools mock

RUN useradd mockbuilder && usermod -a -G mock mockbuilder

# ARG SYSTEM
# COPY packaging/${SYSTEM}/snapd.spec snapd.spec

# RUN dnf -y --refresh install --setopt=install_weak_deps=False $(rpmspec -q --buildrequires snapd.spec)