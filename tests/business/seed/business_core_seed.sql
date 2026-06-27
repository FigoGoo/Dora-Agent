-- Dora business service seed data
-- Owner: 业务微服务后端工程师
-- Scope: local contract and integration tests.
-- Secrets: password fields contain non-production hashes or secret refs only.

INSERT INTO platform_admins (
  id, admin_account, password_hash, display_name, role, status, must_rotate_password
) VALUES (
  'adm_root', 'admin@dora.local', '$argon2id$v=19$m=65536,t=3,p=4$seed$bootstrap', 'Dora Root Admin', 'super_admin', 'active', true
) ON CONFLICT (admin_account) DO NOTHING;

INSERT INTO platform_admin_bootstraps (
  id, admin_id, bootstrap_account, initialized_by, credential_secret_ref, status
) VALUES (
  'adm_bootstrap_001', 'adm_root', 'admin@dora.local', 'system_seed', 'local/bootstrap/admin', 'initialized'
) ON CONFLICT (bootstrap_account) DO NOTHING;

INSERT INTO business_users (
  id, account_no, email, phone, display_name, status, default_space_id, registered_source
) VALUES
  ('usr_1001', 'U1001', 'user1001@dora.local', '+10000000001', 'Seed User', 'active', 'sp_personal_1001', 'seed'),
  ('usr_1002', 'U1002', 'user1002@dora.local', '+10000000002', 'Other Space User', 'active', 'sp_personal_1002', 'seed'),
  ('usr_admin_actor', 'UADMIN', 'admin.actor@dora.local', '+10000000999', 'Admin Actor User', 'active', 'sp_personal_admin_actor', 'seed')
ON CONFLICT (account_no) DO NOTHING;

INSERT INTO business_spaces (
  id, owner_user_id, space_type, enterprise_id, display_name, status, credit_account_id
) VALUES
  ('sp_personal_1001', 'usr_1001', 'personal', null, 'Seed Personal Space', 'active', 'ca_personal_1001'),
  ('sp_personal_1002', 'usr_1002', 'personal', null, 'Other Personal Space', 'active', 'ca_personal_1002'),
  ('sp_personal_admin_actor', 'usr_admin_actor', 'personal', null, 'Admin Actor Space', 'active', null),
  ('sp_enterprise_1001', 'usr_1001', 'enterprise', 'ent_1001', 'Seed Enterprise Space', 'active', 'ca_enterprise_1001')
ON CONFLICT (owner_user_id, space_type, enterprise_id) DO NOTHING;

INSERT INTO enterprises (
  id, enterprise_no, name, owner_user_id, default_space_id, credit_account_id, status
) VALUES (
  'ent_1001', 'E1001', 'Seed Enterprise', 'usr_1001', 'sp_enterprise_1001', 'ca_enterprise_1001', 'active'
) ON CONFLICT (enterprise_no) DO NOTHING;

INSERT INTO enterprise_members (
  id, enterprise_id, user_id, role, status, joined_at
) VALUES (
  'ent_mem_1001_owner', 'ent_1001', 'usr_1001', 'owner', 'active', '2026-06-27T12:00:00Z'
) ON CONFLICT (enterprise_id, user_id) DO NOTHING;

INSERT INTO projects (
  id, project_no, owner_user_id, space_id, enterprise_id, title, status, creative_status, cover_asset_id
) VALUES
  ('prj_active_1001', 'P1001', 'usr_1001', 'sp_personal_1001', null, 'Seed active project', 'active', 'editable', 'ast_generated_1001'),
  ('prj_archived_1001', 'P1002', 'usr_1001', 'sp_personal_1001', null, 'Seed archived project', 'archived', 'locked', null),
  ('prj_other_space_1002', 'P2001', 'usr_1002', 'sp_personal_1002', null, 'Other space project', 'active', 'editable', null)
ON CONFLICT (project_no) DO NOTHING;

INSERT INTO model_providers (
  id, provider_code, display_name, provider_type, status, base_url, config_json, created_by_admin_id
) VALUES (
  'mp_seed', 'seed-provider', 'Seed Provider', 'openai_compatible', 'active', 'http://localhost:18080', '{"timeout_ms":30000}'::jsonb, 'adm_root'
) ON CONFLICT (provider_code) DO NOTHING;

INSERT INTO model_provider_credentials (
  id, provider_id, credential_name, secret_ref, status, created_by_admin_id
) VALUES (
  'mpc_seed', 'mp_seed', 'local-seed', 'local/model-provider/seed', 'active', 'adm_root'
) ON CONFLICT (provider_id, credential_name) DO NOTHING;

INSERT INTO models (
  id, provider_id, model_code, display_name, resource_type, capability_tags, status, credential_id, route_config_json, created_by_admin_id
) VALUES (
  'mdl_seed_image', 'mp_seed', 'seed-image', 'Seed Image Model', 'image', '["image_generation"]'::jsonb, 'active', 'mpc_seed', '{"route":"local"}'::jsonb, 'adm_root'
) ON CONFLICT (provider_id, model_code) DO NOTHING;

INSERT INTO model_prices (
  id, pricing_snapshot_id, model_id, resource_type, billing_unit, unit_points, min_charge_points, status, effective_at, created_by_admin_id
) VALUES (
  'mpr_seed_image', 'price_model_image_seed', 'mdl_seed_image', 'image', 'asset', 12, 0, 'active', '2026-06-27T00:00:00Z', 'adm_root'
) ON CONFLICT (pricing_snapshot_id) DO NOTHING;

INSERT INTO default_models (
  id, resource_type, model_id, pricing_snapshot_id, scope, status, created_by_admin_id
) VALUES (
  'dm_seed_image', 'image', 'mdl_seed_image', 'price_model_image_seed', 'global', 'active', 'adm_root'
) ON CONFLICT (resource_type, scope, status) DO NOTHING;

INSERT INTO tool_definitions (
  id, tool_name, tool_type, display_name, status, version, input_schema_json, output_schema_json, created_by_admin_id
) VALUES
  ('tool_web_fetch', 'web_fetch', 'browser', 'Web Fetch', 'active', '1.0.0', '{"type":"object"}'::jsonb, '{"type":"object"}'::jsonb, 'adm_root'),
  ('tool_remove_bg', 'remove_background', 'image_edit', 'Remove Background', 'active', '1.0.0', '{"type":"object"}'::jsonb, '{"type":"object"}'::jsonb, 'adm_root'),
  ('tool_image_generate', 'image_generate', 'model_generation', 'Image Generate', 'active', '1.0.0', '{"type":"object"}'::jsonb, '{"type":"object"}'::jsonb, 'adm_root')
ON CONFLICT (tool_name, tool_type, version) DO NOTHING;

INSERT INTO tool_policies (
  id, tool_name, tool_type, policy_scope, allowed, risk_level, requires_confirmation, timeout_ms, retry_policy_json, cancel_policy_json, status, changed_by_admin_id
) VALUES
  ('tpol_web_fetch', 'web_fetch', 'browser', 'global', true, 'medium', true, 30000, '{"max_retries":1}'::jsonb, '{"cancelable":true}'::jsonb, 'active', 'adm_root'),
  ('tpol_remove_bg', 'remove_background', 'image_edit', 'global', true, 'low', false, 60000, '{"max_retries":1}'::jsonb, '{"cancelable":true}'::jsonb, 'active', 'adm_root'),
  ('tpol_image_generate', 'image_generate', 'model_generation', 'global', true, 'medium', true, 120000, '{"max_retries":0}'::jsonb, '{"cancelable":true}'::jsonb, 'active', 'adm_root')
ON CONFLICT DO NOTHING;

INSERT INTO tool_pricing_policies (
  id, pricing_policy_id, tool_name, tool_type, charge_mode, billing_unit, unit_points, free_quota, min_charge_points, status, effective_at, changed_by_admin_id
) VALUES
  ('tprice_web_fetch_call', 'tool_price_web_fetch_call', 'web_fetch', 'browser', 'per_call', 'call', 3, 0, 0, 'active', '2026-06-27T00:00:00Z', 'adm_root'),
  ('tprice_remove_bg_asset', 'tool_price_remove_bg_asset', 'remove_background', 'image_edit', 'per_asset', 'asset', 6, 0, 0, 'active', '2026-06-27T00:00:00Z', 'adm_root'),
  ('tprice_storage_free', 'tool_price_storage_free', 'asset_store', 'storage', 'free', 'asset', 0, 999999, 0, 'active', '2026-06-27T00:00:00Z', 'adm_root'),
  ('tprice_image_generate_model', 'tool_price_image_generate_model', 'image_generate', 'model_generation', 'model_generation', 'asset', 12, 0, 0, 'active', '2026-06-27T00:00:00Z', 'adm_root')
ON CONFLICT (pricing_policy_id) DO NOTHING;

INSERT INTO skills (
  id, skill_key, skill_name, skill_scope, owner_user_id, enterprise_id, status, published_version_id, route_hints_json, created_by_user_id
) VALUES (
  'sk_seed_storyboard', 'storyboard', 'Storyboard', 'public', 'usr_1001', null, 'published', 'skv_seed_storyboard_100', '{"intent":"storyboard"}'::jsonb, 'usr_1001'
) ON CONFLICT (skill_key) DO NOTHING;

INSERT INTO skill_versions (
  id, skill_id, version, status, skill_spec_json, input_schema_json, output_schema_json, memory_policy_json, submitted_by_user_id, reviewed_by_admin_id, submitted_at, reviewed_at, published_at
) VALUES (
  'skv_seed_storyboard_100', 'sk_seed_storyboard', '1.0.0', 'published', '{"name":"storyboard","steps":["parse","compose"]}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'
) ON CONFLICT (skill_id, version) DO NOTHING;

INSERT INTO skill_tool_bindings (
  id, skill_id, version_id, tool_name, tool_type, required
) VALUES
  ('sktool_storyboard_image', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'image_generate', 'model_generation', true),
  ('sktool_storyboard_bg', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'remove_background', 'image_edit', false)
ON CONFLICT (version_id, tool_name, tool_type) DO NOTHING;

INSERT INTO skill_output_element_schemas (
  id, skill_id, version_id, element_type, schema_json, required
) VALUES (
  'skel_storyboard_image', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'image.primary', '{"type":"object"}'::jsonb, true
) ON CONFLICT (version_id, element_type) DO NOTHING;

INSERT INTO skill_test_cases (
  id, skill_id, version_id, case_name, test_input_json, expected_elements_json, status, created_by_user_id
) VALUES (
  'skcase_storyboard_basic', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'basic storyboard', '{"prompt":"make a product storyboard"}'::jsonb, '[{"element_type":"image.primary"}]'::jsonb, 'active', 'usr_1001'
) ON CONFLICT DO NOTHING;

INSERT INTO skill_test_runs (
  id, skill_id, version_id, test_case_id, status, execution_mode, input_json, created_by_user_id
) VALUES (
  'skrun_storyboard_001', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'skcase_storyboard_basic', 'created', 'sandbox', '{"prompt":"make a product storyboard"}'::jsonb, 'usr_1001'
) ON CONFLICT DO NOTHING;

INSERT INTO credit_accounts (
  id, account_type, owner_user_id, enterprise_id, status, available_points, frozen_points, expires_soon_points
) VALUES
  ('ca_personal_1001', 'personal', 'usr_1001', null, 'active', 5000, 0, 200),
  ('ca_personal_1002', 'personal', 'usr_1002', null, 'active', 5, 0, 0),
  ('ca_enterprise_1001', 'enterprise', null, 'ent_1001', 'active', 100000, 0, 0)
ON CONFLICT (account_type, owner_user_id, enterprise_id) DO NOTHING;

INSERT INTO credit_batches (
  id, account_id, batch_type, source_type, source_id, total_points, remaining_points, expires_at, status
) VALUES
  ('cb_personal_1001_seed', 'ca_personal_1001', 'grant', 'seed', 'seed_credit_personal', 5000, 5000, '2026-12-31T23:59:59Z', 'active'),
  ('cb_personal_1002_low', 'ca_personal_1002', 'grant', 'seed', 'seed_credit_low_balance', 5, 5, '2026-12-31T23:59:59Z', 'active'),
  ('cb_enterprise_1001_seed', 'ca_enterprise_1001', 'grant', 'seed', 'seed_credit_enterprise', 100000, 100000, '2026-12-31T23:59:59Z', 'active')
ON CONFLICT DO NOTHING;

INSERT INTO redeem_code_batches (
  id, batch_no, target_type, target_user_id, target_enterprise_id, channel_code, total_codes, points_per_code, expires_at, status, created_by_admin_id
) VALUES
  ('rcb_user_1001', 'RCB-USER-1001', 'user', 'usr_1001', null, 'local', 1, 100, '2026-12-31T23:59:59Z', 'active', 'adm_root'),
  ('rcb_enterprise_1001', 'RCB-ENT-1001', 'enterprise', null, 'ent_1001', 'enterprise-only', 1, 1000, '2026-12-31T23:59:59Z', 'active', 'adm_root')
ON CONFLICT (batch_no) DO NOTHING;

INSERT INTO redeem_codes (
  id, batch_id, code_digest, status, expires_at
) VALUES
  ('rc_user_1001', 'rcb_user_1001', 'sha256:seed-user-code', 'unused', '2026-12-31T23:59:59Z'),
  ('rc_enterprise_1001', 'rcb_enterprise_1001', 'sha256:seed-enterprise-only', 'unused', '2026-12-31T23:59:59Z')
ON CONFLICT (code_digest) DO NOTHING;

INSERT INTO asset_element_types (
  id, element_type, display_name, schema_version, schema_json, status
) VALUES
  ('aet_image_primary', 'image.primary', 'Primary Image', '2026-06-27', '{"type":"object"}'::jsonb, 'active'),
  ('aet_text_caption', 'text.caption', 'Caption', '2026-06-27', '{"type":"object"}'::jsonb, 'active')
ON CONFLICT (element_type) DO NOTHING;

INSERT INTO assets (
  id, asset_no, owner_user_id, space_id, enterprise_id, project_id, asset_type, title, status, visibility, source_type, source_ref_id, content_digest, metadata_json
) VALUES
  ('ast_generated_1001', 'A1001', 'usr_1001', 'sp_personal_1001', null, 'prj_active_1001', 'image', 'Generated seed image', 'active', 'private', 'agent_commit', 'art_1001', 'sha256:artifact-content', '{"width":1024,"height":1024}'::jsonb),
  ('ast_other_space_1002', 'A2001', 'usr_1002', 'sp_personal_1002', null, 'prj_other_space_1002', 'image', 'Cross-space asset', 'active', 'private', 'upload', 'upload_seed_2001', 'sha256:other-space-content', '{}'::jsonb)
ON CONFLICT (asset_no) DO NOTHING;

INSERT INTO asset_storage_objects (
  id, asset_id, bucket, object_key_digest, object_uri, mime_type, size_bytes, checksum, storage_status, preview_uri
) VALUES (
  'aso_generated_1001', 'ast_generated_1001', 'dora-local', 'sha256:object-key-generated-1001', 'tos://dora-local/private/generated/1001.png', 'image/png', 2048, 'sha256:artifact-content', 'available', 'http://localhost:19080/api/assets/ast_generated_1001/preview'
) ON CONFLICT DO NOTHING;

INSERT INTO asset_elements (
  id, asset_id, element_type, element_key, element_summary_json, preview_text, status
) VALUES (
  'asel_generated_1001_primary', 'ast_generated_1001', 'image.primary', 'primary', '{"width":1024,"height":1024}'::jsonb, null, 'active'
) ON CONFLICT (asset_id, element_key) DO NOTHING;

INSERT INTO works (
  id, work_no, project_id, owner_user_id, space_id, title, description, cover_asset_id, status, latest_snapshot_id
) VALUES (
  'wrk_seed_public', 'W1001', 'prj_active_1001', 'usr_1001', 'sp_personal_1001', 'Seed public storyboard', 'A public seed work', 'ast_generated_1001', 'public', 'wps_seed_public'
) ON CONFLICT (work_no) DO NOTHING;

INSERT INTO work_assets (
  id, work_id, asset_id, asset_role, sort_order
) VALUES (
  'wka_seed_public_cover', 'wrk_seed_public', 'ast_generated_1001', 'cover', 0
) ON CONFLICT (work_id, asset_id, asset_role) DO NOTHING;

INSERT INTO work_public_snapshots (
  id, snapshot_id, work_id, share_slug, title, description, cover_asset_id, snapshot_json, share_url, visibility, status, like_count, published_by_user_id, published_at
) VALUES (
  'wps_seed_public_row', 'wps_seed_public', 'wrk_seed_public', 'seed-storyboard', 'Seed public storyboard', 'A public seed work', 'ast_generated_1001', '{"assets":[{"asset_id":"ast_generated_1001","element_type":"image.primary"}]}'::jsonb, 'http://localhost:3000/share/seed-storyboard', 'public', 'published', 1, 'usr_1001', '2026-06-27T12:00:00Z'
) ON CONFLICT (snapshot_id) DO NOTHING;

INSERT INTO work_likes (
  id, snapshot_id, user_id, status
) VALUES (
  'wlike_seed_1002', 'wps_seed_public', 'usr_1002', 'liked'
) ON CONFLICT (snapshot_id, user_id) DO NOTHING;

INSERT INTO work_categories (
  id, category_code, display_name, sort_order, status
) VALUES (
  'wcat_storyboard', 'storyboard', 'Storyboard', 10, 'active'
) ON CONFLICT (category_code) DO NOTHING;

INSERT INTO notifications (
  id, notification_no, recipient_user_id, recipient_space_id, recipient_enterprise_id, notification_type, title, body, jump_type, jump_target_id, jump_payload_json, source_type, source_id, status
) VALUES (
  'ntf_skill_review_001', 'N1001', 'usr_1001', 'sp_personal_1001', null, 'skill.review.approved', 'Skill approved', 'Your storyboard skill has been approved.', 'skill_version', 'skv_seed_storyboard_100', '{"skill_id":"sk_seed_storyboard"}'::jsonb, 'skill_review', 'skv_seed_storyboard_100', 'unread'
) ON CONFLICT (notification_no) DO NOTHING;
