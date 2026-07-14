package main

import (
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
)

func TestParseRoleAdminInputSeparatesGrantAndRevoke(t *testing.T) {
	for _, role := range []string{"skill_reviewer", "skill_governor"} {
		t.Run(role, func(t *testing.T) {
			common := []string{
				"-target-user-id", "019f0000-0000-7000-8000-000000000011",
				"-actor-user-id", "019f0000-0000-7000-8000-000000000012",
				"-role", role, "-reason", "role_onboarding", "-approval-reference", "DEPLOY-123",
			}
			grant, err := parseRoleAdminInput(append([]string{"-action", "grant"}, common...))
			if err != nil || grant.Action != "grant" || grant.Role != role || grant.AssignmentID != "" || grant.ExpectedVersion != 0 {
				t.Fatalf("parse grant input: %+v err=%v", grant, err)
			}
			revokeArgs := append([]string{"-action", "revoke"}, common...)
			revokeArgs = append(revokeArgs, "-assignment-id", "019f0000-0000-7000-8000-000000000013", "-expected-version", "1")
			revoke, err := parseRoleAdminInput(revokeArgs)
			if err != nil || revoke.Action != "revoke" || revoke.Role != role || revoke.AssignmentID == "" || revoke.ExpectedVersion != 1 {
				t.Fatalf("parse revoke input: %+v err=%v", revoke, err)
			}
		})
	}
}

func TestParseRoleAdminInputRejectsAmbiguousOrMissingFields(t *testing.T) {
	for _, args := range [][]string{
		{"-action", "unknown"},
		{"-action", "grant", "-target-user-id", "019f0000-0000-7000-8000-000000000011", "-actor-user-id", "019f0000-0000-7000-8000-000000000012", "-role", "unknown_role", "-reason", "role_onboarding", "-approval-reference", "DEPLOY-123"},
		{"-action", "grant", "-assignment-id", "019f0000-0000-7000-8000-000000000013", "-expected-version", "1"},
		{"-action", "revoke", "-target-user-id", "019f0000-0000-7000-8000-000000000011"},
		{"-action", "grant", "trailing"},
	} {
		if _, err := parseRoleAdminInput(args); !errors.Is(err, authorization.ErrInvalidCommand) {
			t.Fatalf("invalid role admin args were accepted: args=%v err=%v", args, err)
		}
	}
}
