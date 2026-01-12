ARG TAG=latest
FROM fedora:${TAG}

RUN dnf makecache && \
    dnf update -y && \
    dnf -y --refresh install --setopt=install_weak_deps=False mock go git

RUN useradd mockbuilder && usermod -a -G mock mockbuilder

USER mockbuilder
