FROM archlinux

COPY packaging/arch/PKGBUILD /root

RUN pacman -Syu --noconfirm && \
    source /root/PKGBUILD && \
    pacman -Suq --needed --noconfirm \
        ${makedepends[@]} \
        ${checkdepends[@]} \
        base-devel

RUN useradd test -m
