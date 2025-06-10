// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package snapadder

import "github.com/snapcore/snapd/client"

func MockIsContainedIn(accepted, candidate string) bool {
	return isContainedIn(accepted, candidate)
}

func MockIsAllowedTopic(snapClient *client.Client, topic, snapName, snapPublisher, action string) (bool, error) {
	return isAllowedTopic(snapClient, topic, snapName, snapPublisher, action)
}
