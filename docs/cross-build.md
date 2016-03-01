# armhf

To cross build for arm you need to install:

    sudo apt-get install golang-go-linux-arm
    sudo apt-get install gcc-arm-linux-gnueabihf

And then set up your environment:

    export GOARCH=arm GOARM=7 CGO_ENABLED=1 CC=arm-linux-gnueabihf-gcc

With that, `go build` will produce binaries for armhf. E.g.,

    go build -o snappy_armhf github.com/ubuntu-core/snappy/cmd/snappy


As usual, for one-off commands you can simply prepend the environment
to the command, e.g.

    GOARCH=arm GOARM=7 CGO_ENABLED=1 CC=arm-linux-gnueabihf-gcc go build -o snappy_armhf github.com/ubuntu-core/snappy/cmd/snappy


# arm64

Install:

    sudo apt-get install gcc-aarch64-linux-gnu

Setup the environment:

    export GOARCH=arm64 CC=aarch64-linux-gnu-gcc CGO_ENABLED=1

And then run:

    go build -o snappy_arm64 github.com/ubuntu-core/snappy/cmd/snappy
