package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/textmaterial"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var textMaterialReadColumns = []string{
	"asset_id", "owner_user_id", "project_id", "asset_version", "media_type", "status",
	"evidence_id", "evidence_media_type", "evidence_kind", "availability", "content_digest",
	"extractor_schema_version", "extractor_version", "locator_kind", "text_start", "text_end",
	"text_source_length", "content", "created_at", "evidence_created_at", "evidence_count",
}

// newTextMaterialRepositoryTestDB 创建由 sqlmock 驱动的 GORM Repository，用于验证事务和 SQL 次数。
func newTextMaterialRepositoryTestDB(t *testing.T) (*TextMaterialRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create sqlmock database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm with sqlmock: %v", err)
	}
	repository, err := NewTextMaterialRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("create text material repository: %v", err)
	}
	return repository, mock
}

// textMaterialFixture 创建满足 Asset/Evidence 完整覆盖不变量的测试素材。
func textMaterialFixture(t *testing.T) textmaterial.TextMaterial {
	t.Helper()
	content := "用于素材分析的完整正文"
	return textmaterial.TextMaterial{
		AssetID: newRepositoryTestUUIDv7(t), EvidenceID: newRepositoryTestUUIDv7(t),
		OwnerUserID: newRepositoryTestUUIDv7(t), ProjectID: newRepositoryTestUUIDv7(t),
		AssetVersion: 1, ContentDigest: textmaterial.ContentDigest(content), Content: content,
		CreatedAt: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC),
	}
}

// expectTextMaterialOwnedProject 期望事务内按可信 owner 读取 active/archived Project。
func expectTextMaterialOwnedProject(mock sqlmock.Sqlmock, material textmaterial.TextMaterial) {
	mock.ExpectQuery(`SELECT .* FROM "business"\."project" WHERE .*id.*owner_user_id.*lifecycle_status.*LIMIT`).
		WithArgs(material.ProjectID, material.OwnerUserID, "active", "archived", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "title", "lifecycle_status", "recent_run_status", "initial_prompt_status", "version", "created_at", "updated_at",
		}).AddRow(
			material.ProjectID, material.OwnerUserID, "项目", "active", "idle", "absent", int64(1), material.CreatedAt, material.CreatedAt,
		))
}

// textMaterialReadRow 把测试素材转换为 Repository 聚合读取的一行。
func textMaterialReadRow(material textmaterial.TextMaterial) *sqlmock.Rows {
	length := int64(len([]rune(material.Content)))
	return sqlmock.NewRows(textMaterialReadColumns).AddRow(
		material.AssetID, material.OwnerUserID, material.ProjectID, material.AssetVersion, "text", "ready",
		material.EvidenceID, "text", "text_segment", "ready", material.ContentDigest,
		textmaterial.ExtractorSchemaVersion, textmaterial.ExtractorVersion, "text_range", int64(0), length,
		length, material.Content, material.CreatedAt, material.CreatedAt, int64(1),
	)
}

func TestTextMaterialRepositoryCreatesAssetAndEvidenceInOneTransaction(t *testing.T) {
	repository, mock := newTextMaterialRepositoryTestDB(t)
	material := textMaterialFixture(t)
	mock.ExpectBegin()
	expectTextMaterialOwnedProject(mock, material)
	mock.ExpectExec(`INSERT INTO "business"\."asset_analysis_preview_assets".*ON CONFLICT.*DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "business"\."asset_analysis_preview_evidence"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.CreateOrReplay(context.Background(), material)
	if err != nil {
		t.Fatalf("CreateOrReplay() error = %v", err)
	}
	if result.Replayed || result.Material.AssetID != material.AssetID {
		t.Fatalf("unexpected result: %+v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTextMaterialRepositoryReplaysSameContentAndRejectsDifferentContent(t *testing.T) {
	for _, test := range []struct {
		name       string
		change     func(*textmaterial.TextMaterial)
		wantReplay bool
		wantErr    error
	}{
		{name: "same content", wantReplay: true},
		{name: "different content", change: func(material *textmaterial.TextMaterial) {
			material.Content = "不同正文"
			material.ContentDigest = textmaterial.ContentDigest(material.Content)
		}, wantErr: textmaterial.ErrIdempotencyConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, mock := newTextMaterialRepositoryTestDB(t)
			persisted := textMaterialFixture(t)
			requested := persisted
			requested.EvidenceID = newRepositoryTestUUIDv7(t)
			if test.change != nil {
				test.change(&requested)
			}
			mock.ExpectBegin()
			expectTextMaterialOwnedProject(mock, requested)
			mock.ExpectExec(`INSERT INTO "business"\."asset_analysis_preview_assets".*ON CONFLICT.*DO NOTHING`).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(`(?s)SELECT.*FROM business\.asset_analysis_preview_assets AS asset.*WHERE asset\.id =.*LIMIT 2`).
				WithArgs(requested.AssetID).WillReturnRows(textMaterialReadRow(persisted))
			if test.wantErr != nil {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}

			result, err := repository.CreateOrReplay(context.Background(), requested)
			if !errors.Is(err, test.wantErr) || result.Replayed != test.wantReplay {
				t.Fatalf("result=%+v error=%v want=%v", result, err, test.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestTextMaterialRepositoryListUsesFixedOwnerAndBoundedCollectionSQL(t *testing.T) {
	repository, mock := newTextMaterialRepositoryTestDB(t)
	material := textMaterialFixture(t)
	expectTextMaterialOwnedProject(mock, material)
	mock.ExpectQuery(regexp.QuoteMeta("WITH selected_assets AS")).
		WithArgs(material.ProjectID, material.OwnerUserID, material.ProjectID, material.OwnerUserID, textmaterial.MaxListItems).
		WillReturnRows(textMaterialReadRow(material))

	items, err := repository.ListOwned(context.Background(), textmaterial.ListQuery{
		OwnerUserID: material.OwnerUserID, ProjectID: material.ProjectID, Limit: textmaterial.MaxListItems,
	})
	if err != nil {
		t.Fatalf("ListOwned() error = %v", err)
	}
	if len(items) != 1 || items[0].Content != material.Content {
		t.Fatalf("unexpected items: %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTextMaterialRepositoryListHidesMissingOrForeignProject(t *testing.T) {
	repository, mock := newTextMaterialRepositoryTestDB(t)
	material := textMaterialFixture(t)
	mock.ExpectQuery(`SELECT .* FROM "business"\."project" WHERE .*id.*owner_user_id.*lifecycle_status.*LIMIT`).
		WithArgs(material.ProjectID, material.OwnerUserID, "active", "archived", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "title", "lifecycle_status", "recent_run_status", "initial_prompt_status", "version", "created_at", "updated_at",
		}))

	items, err := repository.ListOwned(context.Background(), textmaterial.ListQuery{
		OwnerUserID: material.OwnerUserID, ProjectID: material.ProjectID, Limit: textmaterial.MaxListItems,
	})
	if !errors.Is(err, textmaterial.ErrProjectNotFound) || len(items) != 0 {
		t.Fatalf("ListOwned() items=%+v error=%v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
