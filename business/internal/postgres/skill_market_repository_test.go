package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

type marketRepositoryFixture struct {
	SkillID     string
	OwnerID     string
	SnapshotID  string
	SourceID    string
	ReviewID    string
	ReviewerID  string
	PublishedAt time.Time
	Canonical   []byte
	Digest      skill.Digest
	Definition  skill.SkillDefinitionV1
	DisplayName string
}

func newMarketRepositoryFixture(t *testing.T) marketRepositoryFixture {
	t.Helper()
	definition, err := skill.NormalizeDefinitionV1(skillRepositoryDefinition())
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := skill.CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	return marketRepositoryFixture{
		SkillID: newSkillRepositoryUUIDv7(t), OwnerID: newSkillRepositoryUUIDv7(t),
		SnapshotID: newSkillRepositoryUUIDv7(t), SourceID: newSkillRepositoryUUIDv7(t),
		ReviewID: newSkillRepositoryUUIDv7(t), ReviewerID: newSkillRepositoryUUIDv7(t),
		PublishedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), Canonical: canonical,
		Digest: digest, Definition: definition, DisplayName: "Market Publisher",
	}
}

func marketReadColumns() []string {
	return []string{
		"skill_id", "owner_user_id", "current_published_snapshot_id", "skill_publication_revision", "governance_status",
		"published_id", "published_skill_id", "published_source_revision_id", "published_review_id",
		"published_publication_revision", "published_definition_schema", "published_definition_json",
		"published_content_digest", "published_by_user_id", "published_at", "source_revision_id",
		"source_skill_id", "source_content_digest", "publisher_id", "publisher_display_name",
	}
}

func addMarketReadRow(rows *sqlmock.Rows, fixture marketRepositoryFixture, publishedDigest []byte) *sqlmock.Rows {
	return rows.AddRow(
		fixture.SkillID, fixture.OwnerID, fixture.SnapshotID, int64(1), skill.GovernanceStatusActive,
		fixture.SnapshotID, fixture.SkillID, fixture.SourceID, fixture.ReviewID, int64(1),
		skill.DefinitionSchemaVersionV1, fixture.Canonical, publishedDigest, fixture.ReviewerID, fixture.PublishedAt,
		fixture.SourceID, fixture.SkillID, fixture.Digest[:], fixture.OwnerID, fixture.DisplayName,
	)
}

func marketReadRows(fixture marketRepositoryFixture) *sqlmock.Rows {
	return addMarketReadRow(sqlmock.NewRows(marketReadColumns()), fixture, fixture.Digest[:])
}

func TestSkillMarketRepositoryListAndDetailUseOneCurrentPublishedQuery(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newMarketRepositoryFixture(t)
	mock.ExpectQuery(`(?s)WITH market_dangling AS MATERIALIZED.*FROM business\.skill AS skill_record.*published_record\.id IS NULL.*market_valid AS MATERIALIZED.*FROM business\.skill_published_snapshot AS published_record.*ORDER BY published_record\.published_at DESC, published_record\.skill_id DESC.*LIMIT \$1.*UNION ALL.*LIMIT \$2`).
		WithArgs(21, 21).WillReturnRows(marketReadRows(fixture))
	page, err := repository.ListPublished(context.Background(), nil, 20)
	if err != nil || page.HasMore || len(page.Items) != 1 || page.Items[0].SkillID != fixture.SkillID ||
		page.Items[0].PublisherID != fixture.OwnerID || page.Items[0].Definition.Name != fixture.Definition.Name {
		t.Fatalf("Market list page=%+v err=%v", page, err)
	}

	mock.ExpectQuery(`(?s)FROM business\.skill AS skill_record.*WHERE skill_record\.id = \$1.*governance_status = 'active'.*current_published_snapshot_id IS NOT NULL`).
		WithArgs(fixture.SkillID).WillReturnRows(marketReadRows(fixture))
	detail, err := repository.FindPublishedByID(context.Background(), fixture.SkillID)
	if err != nil || detail.SkillID != fixture.SkillID || detail.PublisherDisplayName != fixture.DisplayName ||
		!detail.PublishedAt.Equal(fixture.PublishedAt) {
		t.Fatalf("Market detail=%+v err=%v", detail, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillMarketRepositoryCursorRetainsDanglingPointerClause(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newMarketRepositoryFixture(t)
	boundary := &skill.MarketPageBoundary{PublishedAt: fixture.PublishedAt.Add(time.Second), SkillID: newSkillRepositoryUUIDv7(t)}
	mock.ExpectQuery(`(?s)market_dangling AS MATERIALIZED.*published_record\.id IS NULL.*market_valid AS MATERIALIZED.*\(published_record\.published_at, published_record\.skill_id\) < \(\$1, \$2\).*LIMIT \$3.*UNION ALL.*LIMIT \$4`).
		WithArgs(boundary.PublishedAt, boundary.SkillID, 21, 21).WillReturnRows(marketReadRows(fixture))
	if _, err := repository.ListPublished(context.Background(), boundary, 20); err != nil {
		t.Fatalf("cursor list error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillMarketRepositoryValidatesLookaheadBeforeTruncation(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	rows := sqlmock.NewRows(marketReadColumns())
	for index := 0; index < 21; index++ {
		fixture := newMarketRepositoryFixture(t)
		fixture.PublishedAt = fixture.PublishedAt.Add(-time.Duration(index) * time.Second)
		digest := fixture.Digest[:]
		if index == 20 {
			digest = make([]byte, len(fixture.Digest))
		}
		addMarketReadRow(rows, fixture, digest)
	}
	mock.ExpectQuery(`(?s)WITH market_dangling AS MATERIALIZED.*market_valid AS MATERIALIZED.*LIMIT \$1.*UNION ALL.*LIMIT \$2`).WithArgs(21, 21).WillReturnRows(rows)
	if _, err := repository.ListPublished(context.Background(), nil, 20); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("corrupt lookahead error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillMarketRepositoryDistinguishesInvisibleFromDangling(t *testing.T) {
	repository, mock := newSkillRepositoryTestDB(t)
	fixture := newMarketRepositoryFixture(t)
	mock.ExpectQuery(`(?s)WHERE skill_record\.id = \$1.*governance_status = 'active'`).WithArgs(fixture.SkillID).
		WillReturnRows(sqlmock.NewRows(marketReadColumns()))
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrMarketNotFound) {
		t.Fatalf("invisible detail error = %v", err)
	}

	mock.ExpectQuery(`(?s)WHERE skill_record\.id = \$1.*governance_status = 'active'`).WithArgs(fixture.SkillID).
		WillReturnRows(sqlmock.NewRows(marketReadColumns()).AddRow(
			fixture.SkillID, fixture.OwnerID, fixture.SnapshotID, int64(1), skill.GovernanceStatusActive,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixture.OwnerID, fixture.DisplayName,
		))
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("dangling detail error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
