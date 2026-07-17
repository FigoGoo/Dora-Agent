package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var assetAnalysisCollectionColumns = []string{
	"id", "asset_version", "media_type", "created_at", "id", "asset_id", "asset_version", "media_type",
	"evidence_kind", "availability", "reason_code", "content_digest", "extractor_schema_version", "extractor_version",
	"locator_kind", "text_start", "text_end", "text_source_length", "image_x", "image_y", "image_width", "image_height",
	"content", "created_at",
}

func newAssetAnalysisRepositoryTestDB(t *testing.T) (*AssetAnalysisRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	repository, err := NewAssetAnalysisRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewAssetAnalysisRepository() error = %v", err)
	}
	return repository, mock
}

func TestAssetAnalysisRepositoryUsesOneAuthorizedCollectionSQL(t *testing.T) {
	repository, mock := newAssetAnalysisRepositoryTestDB(t)
	userID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	assetID1 := newRepositoryTestUUIDv7(t)
	assetID2 := newRepositoryTestUUIDv7(t)
	evidenceID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC()
	rows := sqlmock.NewRows(assetAnalysisCollectionColumns).
		AddRow(assetID1, int64(1), "text", now, evidenceID, assetID1, int64(1), "text", "text_segment", "missing",
			"NOT_EXTRACTED", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, now).
		AddRow(assetID2, int64(2), "image", now, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mock.ExpectQuery(`(?s)SELECT.*FROM business\.asset_analysis_preview_assets AS asset.*JOIN business\.project AS project.*LEFT JOIN business\.asset_analysis_preview_evidence AS evidence.*asset\.id IN \(\$5,\$6\).*ORDER BY`).
		WithArgs(projectID, userID, projectID, userID, assetID1, assetID2).
		WillReturnRows(rows)

	assets, err := repository.BatchGetAuthorized(context.Background(), assetanalysis.RepositoryQuery{
		UserID: userID, ProjectID: projectID, AssetIDs: []string{assetID1, assetID2},
	})
	if err != nil {
		t.Fatalf("BatchGetAuthorized() error = %v", err)
	}
	if len(assets) != 2 || len(assets[0].Evidence) != 1 || assets[0].Evidence[0].ReasonCode != "NOT_EXTRACTED" ||
		len(assets[1].Evidence) != 0 {
		t.Fatalf("unexpected assets: %+v", assets)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAssetAnalysisRepositoryMapsDatabaseFailure(t *testing.T) {
	repository, mock := newAssetAnalysisRepositoryTestDB(t)
	ids := []string{newRepositoryTestUUIDv7(t), newRepositoryTestUUIDv7(t), newRepositoryTestUUIDv7(t)}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).WillReturnError(errors.New("database unavailable"))
	_, err := repository.BatchGetAuthorized(context.Background(), assetanalysis.RepositoryQuery{
		UserID: ids[0], ProjectID: ids[1], AssetIDs: []string{ids[2]},
	})
	if !errors.Is(err, assetanalysis.ErrPersistence) {
		t.Fatalf("database error = %v", err)
	}
}
