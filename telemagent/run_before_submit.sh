export GOPATH=$HOME/go
export PATH=$PATH:$GOROOT/bin:$GOPATH/bin

echo "Linting code ..." 
gci write ./
gofumpt -l -w .
goimports -w ./

echo "Running golangci-lint .." 
golangci-lint run


echo "Running unit tests .." 
ping -q google.com &
non_snap_pid=$!
sleep 1
sudo go test ./... -coverprofile=coverage.out
gocov convert ./coverage.out > ./coverage.json
gocov-xml < ./coverage.json > ./coverage.xml
mkdir .coverage
mv ./coverage.xml ./.coverage/

cleanup() {
    echo "Cleaning up..."
    kill -9 $non_snap_pid
}

# Trap the EXIT and ERR signals and call the cleanup function
trap cleanup EXIT