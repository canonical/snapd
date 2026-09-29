ARG SYSTEM=ubuntu
ARG TAG=24.04
FROM ${SYSTEM}:${TAG}

ARG SYSTEM
ARG TAG
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get upgrade -y

RUN if [ "$SYSTEM" = "ubuntu" ] && [ "$TAG" = "16.04" ]; then \
        apt install -y software-properties-common && \
        add-apt-repository ppa:snappy-dev/image && \
        apt update; \
    fi

RUN apt-get install -y \
    sbuild \
    devscripts \
    git

COPY ./debian/control debian/control

RUN apt build-dep -y ./

# Non-default golang packages (golang-1.xx-go) install go binaries into
# /usr/lib/go-1.xx/bin, which is not on PATH. Resolve the most recent one
# at build time and append it to PATH. BASH_ENV makes non-interactive
# shells (docker run ... bash -c) pick it up; profile.d covers login
# shells (su -l).
RUN gobin=$(ls -d /usr/lib/go-*/bin | sort -V | tail -n1) && \
    echo "PATH=\"\$PATH:$gobin\"" > /etc/profile.d/go-path.sh
ENV BASH_ENV=/etc/profile.d/go-path.sh

RUN useradd test -m
