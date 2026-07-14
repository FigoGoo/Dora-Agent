//go:build localsmoke

package main

import "testing"

func TestDistinctSmokeUsersRejectsSharedOrInvalidIdentity(t *testing.T) {
	creator := smokeUserConfig{Email: "creator@example.test", Password: "creator-password", DisplayName: "Creator"}
	reviewer := smokeUserConfig{Email: "reviewer@example.test", Password: "reviewer-password", DisplayName: "Reviewer"}
	provisioner := smokeUserConfig{Email: "provisioner@example.test", Password: "provisioner-password", DisplayName: "Provisioner"}
	if !distinctSmokeUsers(creator, reviewer, provisioner) {
		t.Fatal("three distinct smoke users were rejected")
	}
	reviewer.Email = " CREATOR@EXAMPLE.TEST "
	if distinctSmokeUsers(creator, reviewer, provisioner) {
		t.Fatal("same normalized Creator/Reviewer email was accepted")
	}
}
