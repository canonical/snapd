# live-check observes all .go files outside of the vendor tree and whenever any
# of them changes runs "go test ./..." which tests the whole tree. This is
# handy to keep running in a separate window (on a 2nd monitor perhaps) while
# editing code as it allows one to not have to switch to another application to
# see the general status of unit tests.
.PHONY: live-check
live-check:
	find -name "*.go" \! -path "./vendor/*" | entr -c go test ./...
