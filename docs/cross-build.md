To cross build on arm you need to install:

    sudo apt-get install golang-go-linux-arm
    sudo apt-get install gcc-arm-linux-gnueabihf

And then run:

    GOARM=7 CGO_ENABLED=1 GOARCH=arm go build

