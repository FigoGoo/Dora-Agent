package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newAuthorizationRepositoryTestDB 创建 sqlmock 驱动的授权 Repository。
func newAuthorizationRepositoryTestDB(t *testing.T) (*AuthorizationRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create authorization sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open authorization gorm sqlmock: %v", err)
	}
	repository, err := NewAuthorizationRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("create authorization repository: %v", err)
	}
	return repository, mock
}

func TestAuthorizationRepositoryResolveUsesOneAggregateQuery(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		rolesJSON  string
		wantRoles  int
		wantActive bool
	}{
		{name: "no roles", rolesJSON: `[]`, wantRoles: 0, wantActive: true},
		{name: "reviewer", rolesJSON: `["skill_reviewer"]`, wantRoles: 1, wantActive: true},
		{name: "reviewer and governor", rolesJSON: `["skill_governor","skill_reviewer"]`, wantRoles: 2, wantActive: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, mock := newAuthorizationRepositoryTestDB(t)
			userID := "019f0000-0000-7000-8000-000000000011"
			mock.ExpectQuery(`(?s)jsonb_agg\(DISTINCT assignment\.role_key.*FROM business\.user_account AS account.*LEFT JOIN business\.user_role_assignment`).
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"user_id", "role_keys_json"}).AddRow(userID, []byte(testCase.rolesJSON)))
			resolution, err := repository.ResolveActiveRoles(context.Background(), userID)
			if err != nil || resolution.SubjectActive != testCase.wantActive || len(resolution.Roles) != testCase.wantRoles {
				t.Fatalf("ResolveActiveRoles() = %+v, %v", resolution, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("authorization resolver executed unexpected SQL count: %v", err)
			}
		})
	}
}

func TestAuthorizationRepositoryResolveInactiveAndMalformedFailClosed(t *testing.T) {
	userID := "019f0000-0000-7000-8000-000000000011"
	t.Run("inactive", func(t *testing.T) {
		repository, mock := newAuthorizationRepositoryTestDB(t)
		mock.ExpectQuery(`(?s)FROM business\.user_account AS account`).WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "role_keys_json"}))
		resolution, err := repository.ResolveActiveRoles(context.Background(), userID)
		if err != nil || resolution.SubjectActive || resolution.Roles == nil {
			t.Fatalf("inactive resolution did not return stable empty fact: %+v err=%v", resolution, err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		repository, mock := newAuthorizationRepositoryTestDB(t)
		mock.ExpectQuery(`(?s)FROM business\.user_account AS account`).WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "role_keys_json"}).AddRow(userID, []byte(`{"role":"bad"}`)))
		_, err := repository.ResolveActiveRoles(context.Background(), userID)
		if !errors.Is(err, authorization.ErrUnavailable) {
			t.Fatalf("malformed role aggregate did not fail closed: %v", err)
		}
	})
}
