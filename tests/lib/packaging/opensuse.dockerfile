FROM opensuse/tumbleweed

RUN zypper --gpg-auto-import-keys refresh && \
    zypper in -y --no-recommends osc build rpm-build quilt sudo obs-service-download_files

