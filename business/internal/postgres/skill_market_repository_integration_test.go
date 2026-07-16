package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"gorm.io/gorm"
)

func TestSkillMarketRepositoryPostgreSQLVisibilityAndPublisherPolicy(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedSkillMarketIntegrationPublished(t, db)

	page, err := repository.ListPublished(context.Background(), nil, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].SkillID != fixture.SkillID ||
		page.Items[0].Definition.Name != fixture.PublishedName {
		t.Fatalf("initial Market list page=%+v err=%v", page, err)
	}
	detail, err := repository.FindPublishedByID(context.Background(), fixture.SkillID)
	if err != nil || detail.PublisherDisplayName != fixture.PublisherName || detail.Definition.Name != fixture.PublishedName {
		t.Fatalf("initial Market detail=%+v err=%v", detail, err)
	}

	newDraft := newSkillRepositoryRevision(t, fixture.SkillID, fixture.OwnerID, 2, "未发布的新草稿")
	if err := db.Create(&skillContentRevisionModel{
		ID: newDraft.ID, SkillID: newDraft.SkillID, RevisionNo: newDraft.RevisionNo,
		DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1, DefinitionJSON: append(jsonbValue(nil), newDraft.CanonicalJSON...),
		ContentDigest: digestBytes(newDraft.ContentDigest), CreatedByUserID: fixture.OwnerID, CreatedAt: newDraft.CreatedAt,
	}).Error; err != nil {
		t.Fatalf("insert unpublished Market draft: %v", err)
	}
	if err := db.Exec(`UPDATE business.skill SET current_draft_revision_id = ?, updated_at = ? WHERE id = ?`,
		newDraft.ID, newDraft.CreatedAt, fixture.SkillID).Error; err != nil {
		t.Fatalf("switch unpublished Market draft: %v", err)
	}
	detail, err = repository.FindPublishedByID(context.Background(), fixture.SkillID)
	if err != nil || detail.Definition.Name != fixture.PublishedName {
		t.Fatalf("Market leaked unpublished draft: detail=%+v err=%v", detail, err)
	}

	if err := db.Exec(`UPDATE business.user_account SET status = 'disabled', updated_at = ? WHERE id = ?`,
		fixture.PublishedAt.Add(time.Minute), fixture.OwnerID).Error; err != nil {
		t.Fatalf("disable Market publisher: %v", err)
	}
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); err != nil {
		t.Fatalf("disabled Publisher unexpectedly hid active Skill: %v", err)
	}
	if err := db.Exec(`UPDATE business.user_account SET status = 'cancelled', updated_at = ? WHERE id = ?`,
		fixture.PublishedAt.Add(2*time.Minute), fixture.OwnerID).Error; err != nil {
		t.Fatalf("cancel Market publisher: %v", err)
	}
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); err != nil {
		t.Fatalf("cancelled Publisher unexpectedly hid active Skill: %v", err)
	}

	if err := db.Exec(`UPDATE business.skill SET governance_status = 'suspended', governance_epoch = governance_epoch + 1 WHERE id = ?`, fixture.SkillID).Error; err != nil {
		t.Fatalf("suspend Market Skill: %v", err)
	}
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrMarketNotFound) {
		t.Fatalf("suspended Market detail error = %v", err)
	}
	page, err = repository.ListPublished(context.Background(), nil, 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("suspended Market Skill remained in list: page=%+v err=%v", page, err)
	}

	if err := db.Exec(`UPDATE business.skill SET governance_status = 'active', governance_epoch = governance_epoch + 1 WHERE id = ?`, fixture.SkillID).Error; err != nil {
		t.Fatalf("resume Market Skill: %v", err)
	}
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); err != nil {
		t.Fatalf("resumed Market Skill did not reappear: %v", err)
	}

	danglingID := newSkillRepositoryUUIDv7(t)
	if err := db.Exec(`UPDATE business.skill SET current_published_snapshot_id = ? WHERE id = ?`, danglingID, fixture.SkillID).Error; err != nil {
		t.Fatalf("create dangling Market pointer: %v", err)
	}
	if _, err := repository.FindPublishedByID(context.Background(), fixture.SkillID); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("dangling Market pointer error = %v", err)
	}
	if _, err := repository.ListPublished(context.Background(), &skill.MarketPageBoundary{
		PublishedAt: fixture.PublishedAt.Add(-time.Hour), SkillID: newSkillRepositoryUUIDv7(t),
	}, 20); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("cursor page hid dangling Market pointer: %v", err)
	}
}

func TestSkillMarketRepositoryPostgreSQLValidatesTwentyFirstCandidate(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	fixtures := make([]skillMarketIntegrationFixture, 20)
	for index := range fixtures {
		fixtures[index] = seedSkillMarketIntegrationPublished(t, db)
	}
	sort.Slice(fixtures, func(left int, right int) bool { return fixtures[left].SkillID > fixtures[right].SkillID })
	seen := make(map[string]struct{}, len(fixtures))
	var boundary *skill.MarketPageBoundary
	for {
		page, listErr := repository.ListPublished(context.Background(), boundary, 5)
		if listErr != nil {
			t.Fatalf("read real PostgreSQL Market keyset page: %v", listErr)
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.SkillID]; duplicate {
				t.Fatalf("real PostgreSQL Market keyset duplicated %s", item.SkillID)
			}
			seen[item.SkillID] = struct{}{}
		}
		if !page.HasMore {
			break
		}
		last := page.Items[len(page.Items)-1]
		boundary = &skill.MarketPageBoundary{PublishedAt: last.PublishedAt, SkillID: last.SkillID}
	}
	if len(seen) != len(fixtures) {
		t.Fatalf("real PostgreSQL Market keyset lost items: seen=%d want=%d", len(seen), len(fixtures))
	}
	seedSkillMarketExplainFixtures(t, db, 1500)
	assertSkillMarketExplainUsesKeysetIndex(t, db, fixtures[0].PublishedAt.Add(time.Second), fixtures[0].SkillID)

	seedSkillMarketIntegrationPublishedAt(t, db, fixtures[0].PublishedAt.Add(-time.Second), true)
	if _, err := repository.ListPublished(context.Background(), nil, 20); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("twenty-first corrupt Market candidate was truncated before validation: %v", err)
	}
}

func assertSkillMarketExplainUsesKeysetIndex(t *testing.T, db *gorm.DB, boundaryAt time.Time, boundarySkillID string) {
	t.Helper()
	if err := db.Exec(`ANALYZE business.skill`).Error; err != nil {
		t.Fatalf("analyze Market Skill table: %v", err)
	}
	if err := db.Exec(`ANALYZE business.skill_published_snapshot`).Error; err != nil {
		t.Fatalf("analyze Market Published Snapshot table: %v", err)
	}
	var encoded []byte
	if err := db.Raw(`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+skillMarketListQuery(true),
		boundaryAt, boundarySkillID, 21, 21).Row().Scan(&encoded); err != nil {
		t.Fatalf("explain Skill Market keyset query: %v", err)
	}
	var envelopes []skillMarketExplainEnvelope
	if err := json.Unmarshal(encoded, &envelopes); err != nil || len(envelopes) != 1 {
		t.Fatalf("decode Skill Market explain plan: envelopes=%d err=%v plan=%s", len(envelopes), err, encoded)
	}
	foundKeysetIndex := false
	sortCount := 0
	var unboundedSorts []skillMarketExplainNode
	var inspect func(skillMarketExplainNode)
	inspect = func(node skillMarketExplainNode) {
		if node.IndexName == "idx_skill_published_snapshot__published_skill_id" {
			foundKeysetIndex = true
		}
		if node.NodeType == "Sort" {
			sortCount++
			inputRows := node.ActualRows
			for _, child := range node.Plans {
				if child.ActualRows > inputRows {
					inputRows = child.ActualRows
				}
			}
			if inputRows > 22 {
				unboundedSorts = append(unboundedSorts, node)
			}
		}
		for _, child := range node.Plans {
			inspect(child)
		}
	}
	inspect(envelopes[0].Plan)
	if !foundKeysetIndex || len(unboundedSorts) != 0 {
		t.Fatalf("Skill Market plan violated keyset gate: index=%t sorts=%d unbounded=%d\n%s",
			foundKeysetIndex, sortCount, len(unboundedSorts), encoded)
	}
}

type skillMarketExplainEnvelope struct {
	Plan skillMarketExplainNode `json:"Plan"`
}

type skillMarketExplainNode struct {
	NodeType   string                   `json:"Node Type"`
	IndexName  string                   `json:"Index Name"`
	ActualRows float64                  `json:"Actual Rows"`
	Plans      []skillMarketExplainNode `json:"Plans"`
}

func seedSkillMarketExplainFixtures(t *testing.T, db *gorm.DB, count int) {
	t.Helper()
	if count < 1000 {
		t.Fatalf("Skill Market explain fixture count=%d want at least 1000", count)
	}
	normalized, err := skill.NormalizeDefinitionV1(skillRepositoryDefinition())
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := skill.CanonicalDefinitionV1(normalized)
	if err != nil {
		t.Fatal(err)
	}
	const ownerID = "019f5000-0000-7000-8000-000000000001"
	baseTime := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO business.user_account (
			id, user_type, status, version, created_at, updated_at, display_name
		) VALUES (?, 'personal', 'active', 1, ?, ?, '执行计划测试发布者')`, ownerID, baseTime, baseTime).Error; err != nil {
		t.Fatalf("insert Skill Market explain publisher: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO business.skill_content_revision (
			id, skill_id, revision_no, definition_schema_version, definition_json,
			content_digest, created_by_user_id, created_at
		)
		SELECT
			('019f2000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			('019f1000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			1, 'skill_definition.v1', CAST(? AS jsonb), ?, ?::uuid,
			?::timestamptz - make_interval(secs => series_id)
		FROM generate_series(1, ?) AS generated(series_id)`,
		string(canonical), digest[:], ownerID, baseTime, count).Error; err != nil {
		t.Fatalf("batch insert Skill Market explain source revisions: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO business.skill_published_snapshot (
			id, skill_id, source_content_revision_id, review_submission_id, publication_revision,
			definition_schema_version, definition_json, content_digest, published_by_user_id, published_at
		)
		SELECT
			('019f3000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			('019f1000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			('019f2000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			('019f4000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			1, 'skill_definition.v1', CAST(? AS jsonb), ?, ?::uuid,
			?::timestamptz - make_interval(secs => series_id)
		FROM generate_series(1, ?) AS generated(series_id)`,
		string(canonical), digest[:], ownerID, baseTime, count).Error; err != nil {
		t.Fatalf("batch insert Skill Market explain published snapshots: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO business.skill (
			id, owner_user_id, current_draft_revision_id, current_published_snapshot_id,
			publication_revision, governance_status, version, created_at, updated_at
		)
		SELECT
			('019f1000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			?::uuid,
			('019f2000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			('019f3000-0000-7000-8000-' || lpad(to_hex(series_id), 12, '0'))::uuid,
			1, 'active', 1,
			?::timestamptz - make_interval(secs => series_id),
			?::timestamptz - make_interval(secs => series_id)
		FROM generate_series(1, ?) AS generated(series_id)`, ownerID, baseTime, baseTime, count).Error; err != nil {
		t.Fatalf("batch insert Skill Market explain Skills: %v", err)
	}
}

type skillMarketIntegrationFixture struct {
	SkillID       string
	OwnerID       string
	PublishedName string
	PublisherName string
	PublishedAt   time.Time
}

func seedSkillMarketIntegrationPublished(t *testing.T, db *gorm.DB) skillMarketIntegrationFixture {
	t.Helper()
	return seedSkillMarketIntegrationPublishedAt(t, db, time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), false)
}

func seedSkillMarketIntegrationPublishedAt(
	t *testing.T,
	db *gorm.DB,
	publishedAt time.Time,
	corruptSourceDigest bool,
) skillMarketIntegrationFixture {
	t.Helper()
	ownerID := newSkillRepositoryUUIDv7(t)
	skillID := newSkillRepositoryUUIDv7(t)
	sourceID := newSkillRepositoryUUIDv7(t)
	snapshotID := newSkillRepositoryUUIDv7(t)
	reviewID := newSkillRepositoryUUIDv7(t)
	reviewerID := newSkillRepositoryUUIDv7(t)
	now := publishedAt.UTC()
	definition := skillRepositoryDefinition()
	definition.Name = "公开发布版本"
	normalized, err := skill.NormalizeDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := skill.CanonicalDefinitionV1(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&userAccountModel{
		ID: ownerID, DisplayName: "公开发布者", UserType: "personal", Status: "active", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert Market publisher: %v", err)
	}
	sourceDigest := digest
	if corruptSourceDigest {
		sourceDigest = skill.Digest{}
	}
	if err := db.Create(&skillContentRevisionModel{
		ID: sourceID, SkillID: skillID, RevisionNo: 1, DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
		DefinitionJSON: append(jsonbValue(nil), canonical...), ContentDigest: digestBytes(sourceDigest),
		CreatedByUserID: ownerID, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert Market source revision: %v", err)
	}
	if err := db.Create(&skillPublishedSnapshotModel{
		ID: snapshotID, SkillID: skillID, SourceContentRevisionID: sourceID, ReviewSubmissionID: reviewID,
		PublicationRevision: 1, DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
		DefinitionJSON: append(jsonbValue(nil), canonical...), ContentDigest: digestBytes(digest),
		PublishedByUserID: reviewerID, PublishedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert Market published snapshot: %v", err)
	}
	if err := db.Create(&skillModel{
		ID: skillID, OwnerUserID: ownerID, CurrentDraftRevisionID: sourceID,
		CurrentPublishedSnapshotID: &snapshotID, PublicationRevision: 1,
		GovernanceStatus: string(skill.GovernanceStatusActive), Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert Market Skill: %v", err)
	}
	return skillMarketIntegrationFixture{
		SkillID: skillID, OwnerID: ownerID, PublishedName: normalized.Name,
		PublisherName: "公开发布者", PublishedAt: now,
	}
}
