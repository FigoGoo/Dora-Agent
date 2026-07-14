//go:build localsmoke

package main

import "testing"

func TestDistinctSmokeUsersRejectsSharedOrInvalidIdentity(t *testing.T) {
	creator := smokeUserConfig{Email: "creator@example.test", Password: "creator-password", DisplayName: "Creator"}
	reviewer := smokeUserConfig{Email: "reviewer@example.test", Password: "reviewer-password", DisplayName: "Reviewer"}
	governor := smokeUserConfig{Email: "governor@example.test", Password: "governor-password", DisplayName: "Governor"}
	provisioner := smokeUserConfig{Email: "provisioner@example.test", Password: "provisioner-password", DisplayName: "Provisioner"}
	if !distinctSmokeUsers(creator, reviewer, governor, provisioner) {
		t.Fatal("four distinct smoke users were rejected")
	}
	reviewer.Email = " CREATOR@EXAMPLE.TEST "
	if distinctSmokeUsers(creator, reviewer, governor, provisioner) {
		t.Fatal("same normalized Creator/Reviewer email was accepted")
	}
}

func TestDistinctSmokeUserIDsRejectsMergedIdentity(t *testing.T) {
	if !distinctSmokeUserIDs("creator", "reviewer", "governor", "provisioner") {
		t.Fatal("four distinct user IDs were rejected")
	}
	if distinctSmokeUserIDs("creator", "reviewer", "governor", "reviewer") {
		t.Fatal("merged Reviewer/Provisioner identity was accepted")
	}
	if distinctSmokeUserIDs("creator", "reviewer", "", "provisioner") {
		t.Fatal("empty Governor identity was accepted")
	}
}
