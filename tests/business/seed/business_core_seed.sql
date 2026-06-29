-- Dora business service seed data
-- Owner: 业务微服务后端工程师
-- Scope: local contract and integration tests.
-- Secrets: password fields contain non-production hashes or secret refs only.

INSERT INTO platform_admins (
  id, admin_account, password_hash, display_name, role, status, must_rotate_password
) VALUES (
  'adm_root', 'admin@dora.local', '$argon2id$v=19$m=16384,t=1,p=1$ZG9yYS1sb2NhbC1zYWx0MQ$4jdN85WOR//36CwDBXmQQli7Mu8sYwHd+AM3HYmjPXI', 'Dora Root Admin', 'super_admin', 'active', true
) ON CONFLICT (admin_account) DO NOTHING;

INSERT INTO platform_admin_bootstraps (
  id, admin_id, bootstrap_account, initialized_by, credential_secret_ref, status
) VALUES (
  'adm_bootstrap_001', 'adm_root', 'admin@dora.local', 'system_seed', 'local/bootstrap/admin', 'initialized'
) ON CONFLICT (bootstrap_account) DO NOTHING;

INSERT INTO business_users (
  id, account_no, email, phone, email_hash, phone_hash, password_hash, display_name, status, default_space_id, registered_source
) VALUES
  ('usr_1001', 'U1001', 'user1001@dora.local', '+10000000001', 'd354a6b859c939fdfde08f22baee6ebc7d7c5c6538398c905d2abddc43bad1ee', '1fb1f420856780a29719b994c8764b81770d79f97e2e1861ba938a7a5a15dfb9', '$argon2id$v=19$m=16384,t=1,p=1$ZG9yYS11c2VyLXNhbHQwMQ$AfDQPdlOJ78pkBwqpH3UA0UMVwTpuKPCZTx+PvpD6Xw', 'Seed User', 'active', 'sp_personal_1001', 'seed'),
  ('usr_1002', 'U1002', 'user1002@dora.local', '+10000000002', 'c3a48846c3d4d3f2c72fa5a79c1bfd5d75647e43a93084625aa793a7bcca5a14', 'd9d0a321f73cff7a953a6e48ec25c035e515c54181193d5729dd995733af8467', '$argon2id$v=19$m=16384,t=1,p=1$ZG9yYS11c2VyLXNhbHQwMQ$AfDQPdlOJ78pkBwqpH3UA0UMVwTpuKPCZTx+PvpD6Xw', 'Other Space User', 'active', 'sp_personal_1002', 'seed'),
  ('usr_admin_actor', 'UADMIN', 'admin.actor@dora.local', '+10000000999', '7b83f95f80ac466d0a4a969b3512096188ded5a287ed0c91cfecbf2a863b35ae', 'b7322332e5b9388014568a94f1730c771d9720ca4d8f88deb37c739d1ed2ea6a', '$argon2id$v=19$m=16384,t=1,p=1$ZG9yYS11c2VyLXNhbHQwMQ$AfDQPdlOJ78pkBwqpH3UA0UMVwTpuKPCZTx+PvpD6Xw', 'Admin Actor User', 'active', 'sp_personal_admin_actor', 'seed')
ON CONFLICT (account_no) DO NOTHING;

INSERT INTO business_spaces (
  id, owner_user_id, space_type, enterprise_id, display_name, status, credit_account_id
) VALUES
  ('sp_personal_1001', 'usr_1001', 'personal', null, 'Seed Personal Space', 'active', 'ca_personal_1001'),
  ('sp_personal_1002', 'usr_1002', 'personal', null, 'Other Personal Space', 'active', 'ca_personal_1002'),
  ('sp_personal_admin_actor', 'usr_admin_actor', 'personal', null, 'Admin Actor Space', 'active', null),
  ('sp_enterprise_1001', 'usr_1001', 'enterprise', 'ent_1001', 'Seed Enterprise Space', 'active', 'ca_enterprise_1001')
ON CONFLICT (id) DO UPDATE SET
  owner_user_id = EXCLUDED.owner_user_id,
  space_type = EXCLUDED.space_type,
  enterprise_id = EXCLUDED.enterprise_id,
  display_name = EXCLUDED.display_name,
  status = EXCLUDED.status,
  credit_account_id = EXCLUDED.credit_account_id,
  updated_at = now(),
  updated_by = 'seed:business_core';

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
) VALUES
  ('sk_seed_storyboard', 'storyboard', 'Storyboard', 'public', 'usr_1001', null, 'published', 'skv_seed_storyboard_100', '{"intent":"storyboard","keywords":"storyboard,故事板,分镜,镜头,广告短片,广告片,视觉方案,主视觉,product launch video","priority":"80","negative_keywords":"邮件,道歉信,合同,发票,提示词,mj,prompt,关键词,seo,报销"}'::jsonb, 'usr_1001'),
  ('sk_seed_product_copy', 'product_copy', '商品文案', 'public', 'usr_1001', null, 'published', 'skv_seed_product_copy_100', '{"intent":"product_copy","keywords":"商品文案,种草文案,卖点,详情页,短标题,直播间,转化短文案,cta,标题,电商文案,小红书风格","priority":"70","negative_keywords":"分镜,故事板,品牌定位,定位策略,会议纪要,客服,投诉,退款,seo,关键词,数据分析"}'::jsonb, 'usr_1001'),
  ('sk_seed_brand_strategy', 'brand_strategy', '品牌策略', 'public', 'usr_1001', null, 'published', 'skv_seed_brand_strategy_100', '{"intent":"brand_strategy","keywords":"品牌策略,品牌定位,定位策略,目标人群,差异化,品牌语气,人群,brand positioning,tone of voice,brand strategy","priority":"75","negative_keywords":"分镜,故事板,详情页,短标题,社媒日历,内容日历,会议纪要,客服回复,退款,seo文章"}'::jsonb, 'usr_1001'),
  ('sk_seed_social_calendar', 'social_calendar', '社媒内容日历', 'public', 'usr_1001', null, 'published', 'skv_seed_social_calendar_100', '{"intent":"social_calendar","keywords":"社媒日历,内容日历,选题日历,抖音,小红书,公众号排期,每周主题,发布计划,social calendar,content calendar","priority":"68","negative_keywords":"品牌定位,目标人群,seo,搜索收录,会议纪要,客服,退款,发票"}'::jsonb, 'usr_1001'),
  ('sk_seed_seo_article', 'seo_article', 'SEO 长文', 'public', 'usr_1001', null, 'published', 'skv_seed_seo_article_100', '{"intent":"seo_article","keywords":"seo,SEO,搜索收录,关键词,长文结构,文章大纲,小标题,选购指南,搜索排名,search keywords","priority":"72","negative_keywords":"社媒日历,朋友圈,分镜,故事板,投放点击率,转化率,roi,会议纪要,客服"}'::jsonb, 'usr_1001'),
  ('sk_seed_meeting_summary', 'meeting_summary', '会议纪要整理', 'public', 'usr_1001', null, 'published', 'skv_seed_meeting_summary_100', '{"intent":"meeting_summary","keywords":"会议纪要,会议总结,复盘会议,决议,待办,负责人,行动项,纪要整理,meeting notes,action items","priority":"73","negative_keywords":"回复客户,客服回复,营销文案,分镜,故事板,seo,海报,提示词"}'::jsonb, 'usr_1001'),
  ('sk_seed_support_reply', 'support_reply', '客服回复', 'public', 'usr_1001', null, 'published', 'skv_seed_support_reply_100', '{"intent":"support_reply","keywords":"客服回复,客服话术,客户投诉,物流延迟,补偿建议,退款,售后,用户投诉,客诉,customer support","priority":"74","negative_keywords":"会议纪要,复盘会议,品牌定位,商品文案,营销文案,seo,分镜,故事板"}'::jsonb, 'usr_1001'),
  ('sk_seed_data_insight', 'data_insight', '经营数据分析', 'public', 'usr_1001', null, 'published', 'skv_seed_data_insight_100', '{"intent":"data_insight","keywords":"数据分析,经营分析,转化率,客单价,点击率,roi,ROI,投放数据,优化建议,指标解读,data insight","priority":"76","negative_keywords":"seo关键词,搜索收录,会议纪要,客服回复,分镜,故事板,提示词,海报"}'::jsonb, 'usr_1001'),
  ('sk_seed_image_prompt', 'image_prompt', '出图提示词', 'public', 'usr_1001', null, 'published', 'skv_seed_image_prompt_100', '{"intent":"image_prompt","keywords":"出图提示词,提示词,mj,MJ,midjourney,prompt,海报提示词,构图,光影,材质,风格词,negative prompt","priority":"78","negative_keywords":"分镜,故事板,广告片,剧情,会议纪要,客服,seo文章,品牌定位"}'::jsonb, 'usr_1001')
ON CONFLICT (skill_key) DO UPDATE SET
  skill_name = EXCLUDED.skill_name,
  skill_scope = EXCLUDED.skill_scope,
  status = EXCLUDED.status,
  published_version_id = EXCLUDED.published_version_id,
  route_hints_json = EXCLUDED.route_hints_json,
  updated_at = now(),
  updated_by = 'seed:business_core';

INSERT INTO skill_versions (
  id, skill_id, version, status, skill_spec_json, input_schema_json, output_schema_json, memory_policy_json, confirmation_policy_json, submitted_by_user_id, reviewed_by_admin_id, submitted_at, reviewed_at, published_at
) VALUES
  ('skv_seed_storyboard_100', 'sk_seed_storyboard', '1.0.0', 'published', '{"name":"storyboard","steps":["parse","compose"],"business_scenario":"广告短片分镜"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_product_copy_100', 'sk_seed_product_copy', '1.0.0', 'published', '{"name":"product_copy","steps":["extract_selling_points","compose_copy"],"business_scenario":"商品文案生成"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_brand_strategy_100', 'sk_seed_brand_strategy', '1.0.0', 'published', '{"name":"brand_strategy","steps":["define_audience","position_brand","set_tone"],"business_scenario":"品牌定位策略"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_social_calendar_100', 'sk_seed_social_calendar', '1.0.0', 'published', '{"name":"social_calendar","steps":["cluster_topics","schedule_posts"],"business_scenario":"社媒内容排期"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_seo_article_100', 'sk_seed_seo_article', '1.0.0', 'published', '{"name":"seo_article","steps":["extract_keywords","outline_article"],"business_scenario":"SEO 长文规划"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_meeting_summary_100', 'sk_seed_meeting_summary', '1.0.0', 'published', '{"name":"meeting_summary","steps":["summarize_decisions","extract_actions"],"business_scenario":"会议纪要整理"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_support_reply_100', 'sk_seed_support_reply', '1.0.0', 'published', '{"name":"support_reply","steps":["classify_issue","draft_reply"],"business_scenario":"客服售后回复"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_data_insight_100', 'sk_seed_data_insight', '1.0.0', 'published', '{"name":"data_insight","steps":["read_metrics","diagnose_changes","recommend_actions"],"business_scenario":"经营数据洞察"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z'),
  ('skv_seed_image_prompt_100', 'sk_seed_image_prompt', '1.0.0', 'published', '{"name":"image_prompt","steps":["extract_visual_goal","compose_prompt"],"business_scenario":"图片生成提示词"}'::jsonb, '{"type":"object","required":["prompt"]}'::jsonb, '{"type":"object","required":["elements"]}'::jsonb, '{"enabled":true}'::jsonb, '{"requires_confirmation":false,"required_actions":[],"min_confirm_level":"none","lock_fields":[],"expires_in_seconds":900}'::jsonb, 'usr_1001', 'adm_root', '2026-06-27T11:00:00Z', '2026-06-27T11:20:00Z', '2026-06-27T11:30:00Z')
ON CONFLICT (skill_id, version) DO UPDATE SET
  status = EXCLUDED.status,
  skill_spec_json = EXCLUDED.skill_spec_json,
  input_schema_json = EXCLUDED.input_schema_json,
  output_schema_json = EXCLUDED.output_schema_json,
  memory_policy_json = EXCLUDED.memory_policy_json,
  confirmation_policy_json = EXCLUDED.confirmation_policy_json,
  submitted_by_user_id = EXCLUDED.submitted_by_user_id,
  reviewed_by_admin_id = EXCLUDED.reviewed_by_admin_id,
  submitted_at = EXCLUDED.submitted_at,
  reviewed_at = EXCLUDED.reviewed_at,
  published_at = EXCLUDED.published_at,
  updated_at = now(),
  updated_by = 'seed:business_core';

INSERT INTO skill_tool_bindings (
  id, skill_id, version_id, tool_name, tool_type, required
) VALUES
  ('sktool_storyboard_image', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'image_generate', 'model_generation', true),
  ('sktool_storyboard_bg', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'remove_background', 'image_edit', false),
  ('sktool_image_prompt_image', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'image_generate', 'model_generation', false)
ON CONFLICT (version_id, tool_name, tool_type) DO NOTHING;

INSERT INTO skill_output_element_schemas (
  id, skill_id, version_id, element_type, element_name, schema_json, required, use_draft, use_final, editable, referable, display_order, display_slot
) VALUES
  ('skel_storyboard_image', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'image_ref', '故事板参考图', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'asset_detail'),
  ('skel_storyboard_board', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'storyboard', '分镜脚本', '{"type":"object"}'::jsonb, true, true, true, true, true, 20, 'blackboard'),
  ('skel_product_copy_text', 'sk_seed_product_copy', 'skv_seed_product_copy_100', 'rich_text', '商品文案', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_product_copy_tags', 'sk_seed_product_copy', 'skv_seed_product_copy_100', 'tag_group', '卖点标签', '{"type":"object"}'::jsonb, false, true, true, true, true, 20, 'blackboard'),
  ('skel_brand_strategy_doc', 'sk_seed_brand_strategy', 'skv_seed_brand_strategy_100', 'structured_object', '品牌策略卡', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_brand_strategy_text', 'sk_seed_brand_strategy', 'skv_seed_brand_strategy_100', 'long_text', '策略说明', '{"type":"object"}'::jsonb, false, true, true, true, true, 20, 'blackboard'),
  ('skel_social_calendar_list', 'sk_seed_social_calendar', 'skv_seed_social_calendar_100', 'list', '内容日历', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_social_calendar_tags', 'sk_seed_social_calendar', 'skv_seed_social_calendar_100', 'tag_group', '渠道标签', '{"type":"object"}'::jsonb, false, true, true, true, true, 20, 'blackboard'),
  ('skel_seo_article_outline', 'sk_seed_seo_article', 'skv_seed_seo_article_100', 'rich_text', 'SEO 文章大纲', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_seo_article_keywords', 'sk_seed_seo_article', 'skv_seed_seo_article_100', 'tag_group', 'SEO 关键词', '{"type":"object"}'::jsonb, true, true, true, true, true, 20, 'blackboard'),
  ('skel_meeting_summary_text', 'sk_seed_meeting_summary', 'skv_seed_meeting_summary_100', 'rich_text', '会议纪要', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_meeting_summary_actions', 'sk_seed_meeting_summary', 'skv_seed_meeting_summary_100', 'list', '待办列表', '{"type":"object"}'::jsonb, true, true, true, true, true, 20, 'blackboard'),
  ('skel_support_reply_text', 'sk_seed_support_reply', 'skv_seed_support_reply_100', 'rich_text', '客服回复', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_support_reply_policy', 'sk_seed_support_reply', 'skv_seed_support_reply_100', 'structured_object', '补偿建议', '{"type":"object"}'::jsonb, false, true, true, true, true, 20, 'blackboard'),
  ('skel_data_insight_object', 'sk_seed_data_insight', 'skv_seed_data_insight_100', 'structured_object', '经营分析', '{"type":"object"}'::jsonb, true, true, true, true, true, 10, 'blackboard'),
  ('skel_data_insight_actions', 'sk_seed_data_insight', 'skv_seed_data_insight_100', 'list', '优化动作', '{"type":"object"}'::jsonb, false, true, true, true, true, 20, 'blackboard'),
  ('skel_image_prompt_prompt', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'prompt', '出图提示词', '{"type":"object"}'::jsonb, true, true, false, true, true, 10, 'blackboard'),
  ('skel_image_prompt_params', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'parameter_group', '生成参数', '{"type":"object"}'::jsonb, false, true, false, true, true, 20, 'blackboard')
ON CONFLICT (version_id, element_type) DO UPDATE SET
  element_name = EXCLUDED.element_name,
  schema_json = EXCLUDED.schema_json,
  required = EXCLUDED.required,
  use_draft = EXCLUDED.use_draft,
  use_final = EXCLUDED.use_final,
  editable = EXCLUDED.editable,
  referable = EXCLUDED.referable,
  display_order = EXCLUDED.display_order,
  display_slot = EXCLUDED.display_slot,
  updated_by = 'seed:business_core';

INSERT INTO skill_test_cases (
  id, skill_id, version_id, case_name, test_input_json, expected_elements_json, status, created_by_user_id
) VALUES
  ('skcase_storyboard_basic', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'basic storyboard', '{"prompt":"make a product storyboard"}'::jsonb, '[{"element_type":"image_ref"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_storyboard_caption', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'captioned storyboard', '{"prompt":"make a storyboard with captions"}'::jsonb, '[{"element_type":"image_ref"},{"element_type":"short_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_storyboard_variants', 'sk_seed_storyboard', 'skv_seed_storyboard_100', 'variant storyboard', '{"prompt":"make three visual variants"}'::jsonb, '[{"element_type":"storyboard"},{"element_type":"parameter_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_product_copy_basic', 'sk_seed_product_copy', 'skv_seed_product_copy_100', 'product copy basic', '{"prompt":"写商品种草文案"}'::jsonb, '[{"element_type":"rich_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_product_copy_detail', 'sk_seed_product_copy', 'skv_seed_product_copy_100', 'detail page copy', '{"prompt":"写详情页卖点"}'::jsonb, '[{"element_type":"rich_text"},{"element_type":"tag_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_product_copy_live', 'sk_seed_product_copy', 'skv_seed_product_copy_100', 'live copy', '{"prompt":"写直播间转化短文案"}'::jsonb, '[{"element_type":"rich_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_brand_strategy_basic', 'sk_seed_brand_strategy', 'skv_seed_brand_strategy_100', 'brand positioning basic', '{"prompt":"做品牌定位策略"}'::jsonb, '[{"element_type":"structured_object"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_brand_strategy_tone', 'sk_seed_brand_strategy', 'skv_seed_brand_strategy_100', 'brand tone', '{"prompt":"定义品牌语气"}'::jsonb, '[{"element_type":"structured_object"},{"element_type":"long_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_brand_strategy_audience', 'sk_seed_brand_strategy', 'skv_seed_brand_strategy_100', 'brand audience', '{"prompt":"梳理目标人群和差异化"}'::jsonb, '[{"element_type":"structured_object"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_social_calendar_month', 'sk_seed_social_calendar', 'skv_seed_social_calendar_100', 'monthly content calendar', '{"prompt":"规划下个月社媒日历"}'::jsonb, '[{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_social_calendar_channel', 'sk_seed_social_calendar', 'skv_seed_social_calendar_100', 'channel plan', '{"prompt":"安排抖音小红书发布计划"}'::jsonb, '[{"element_type":"list"},{"element_type":"tag_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_social_calendar_topics', 'sk_seed_social_calendar', 'skv_seed_social_calendar_100', 'weekly topics', '{"prompt":"按每周主题做内容排期"}'::jsonb, '[{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_seo_article_outline', 'sk_seed_seo_article', 'skv_seed_seo_article_100', 'seo outline', '{"prompt":"写SEO文章大纲"}'::jsonb, '[{"element_type":"rich_text"},{"element_type":"tag_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_seo_article_keywords', 'sk_seed_seo_article', 'skv_seed_seo_article_100', 'seo keywords', '{"prompt":"整理搜索关键词"}'::jsonb, '[{"element_type":"tag_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_seo_article_guide', 'sk_seed_seo_article', 'skv_seed_seo_article_100', 'buying guide', '{"prompt":"写选购指南长文结构"}'::jsonb, '[{"element_type":"rich_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_meeting_summary_notes', 'sk_seed_meeting_summary', 'skv_seed_meeting_summary_100', 'meeting notes', '{"prompt":"整理会议纪要"}'::jsonb, '[{"element_type":"rich_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_meeting_summary_actions', 'sk_seed_meeting_summary', 'skv_seed_meeting_summary_100', 'action items', '{"prompt":"提取决议待办负责人"}'::jsonb, '[{"element_type":"rich_text"},{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_meeting_summary_review', 'sk_seed_meeting_summary', 'skv_seed_meeting_summary_100', 'review meeting', '{"prompt":"整理复盘会议行动项"}'::jsonb, '[{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_support_reply_complaint', 'sk_seed_support_reply', 'skv_seed_support_reply_100', 'complaint reply', '{"prompt":"写客户投诉回复"}'::jsonb, '[{"element_type":"rich_text"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_support_reply_refund', 'sk_seed_support_reply', 'skv_seed_support_reply_100', 'refund reply', '{"prompt":"回复要求退款的用户"}'::jsonb, '[{"element_type":"rich_text"},{"element_type":"structured_object"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_support_reply_delay', 'sk_seed_support_reply', 'skv_seed_support_reply_100', 'logistics delay', '{"prompt":"物流延迟补偿建议"}'::jsonb, '[{"element_type":"structured_object"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_data_insight_metrics', 'sk_seed_data_insight', 'skv_seed_data_insight_100', 'metric insight', '{"prompt":"分析转化率和客单价"}'::jsonb, '[{"element_type":"structured_object"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_data_insight_ads', 'sk_seed_data_insight', 'skv_seed_data_insight_100', 'ads insight', '{"prompt":"分析投放点击率 ROI"}'::jsonb, '[{"element_type":"structured_object"},{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_data_insight_actions', 'sk_seed_data_insight', 'skv_seed_data_insight_100', 'optimization actions', '{"prompt":"给经营优化建议"}'::jsonb, '[{"element_type":"list"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_image_prompt_basic', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'image prompt basic', '{"prompt":"生成海报出图提示词"}'::jsonb, '[{"element_type":"prompt"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_image_prompt_params', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'image prompt params', '{"prompt":"写MJ提示词和参数"}'::jsonb, '[{"element_type":"prompt"},{"element_type":"parameter_group"}]'::jsonb, 'active', 'usr_1001'),
  ('skcase_image_prompt_negative', 'sk_seed_image_prompt', 'skv_seed_image_prompt_100', 'image negative prompt', '{"prompt":"补充negative prompt"}'::jsonb, '[{"element_type":"prompt"}]'::jsonb, 'active', 'usr_1001')
ON CONFLICT DO NOTHING;

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
ON CONFLICT (id) DO UPDATE SET
  account_type = EXCLUDED.account_type,
  owner_user_id = EXCLUDED.owner_user_id,
  enterprise_id = EXCLUDED.enterprise_id,
  status = EXCLUDED.status,
  available_points = EXCLUDED.available_points,
  frozen_points = EXCLUDED.frozen_points,
  expires_soon_points = EXCLUDED.expires_soon_points,
  updated_at = now(),
  updated_by = 'seed:business_core';

INSERT INTO credit_batches (
  id, account_id, batch_type, source_type, source_id, total_points, remaining_points, expires_at, status
) VALUES
  ('cb_personal_1001_seed', 'ca_personal_1001', 'grant', 'seed', 'seed_credit_personal', 5000, 5000, '2026-12-31T23:59:59Z', 'active'),
  ('cb_personal_1002_low', 'ca_personal_1002', 'grant', 'seed', 'seed_credit_low_balance', 5, 5, '2026-12-31T23:59:59Z', 'active'),
  ('cb_enterprise_1001_seed', 'ca_enterprise_1001', 'grant', 'seed', 'seed_credit_enterprise', 100000, 100000, '2026-12-31T23:59:59Z', 'active')
ON CONFLICT DO NOTHING;

INSERT INTO redeem_code_batches (
  id, batch_no, target_type, account_type, bind_target_type, bind_target_id, target_user_id, target_enterprise_id, channel_code, total_codes, points_per_code, expires_at, credit_expires_at, status, created_by_admin_id, reason
) VALUES
  ('rcb_user_1001', 'RCB-USER-1001', 'user', 'personal', 'user', 'usr_1001', 'usr_1001', null, 'local', 1, 100, '2026-12-31T23:59:59Z', '2026-12-31T23:59:59Z', 'active', 'adm_root', 'seed personal user redeem code'),
  ('rcb_enterprise_1001', 'RCB-ENT-1001', 'enterprise', 'enterprise', 'enterprise', 'ent_1001', null, 'ent_1001', 'enterprise-only', 1, 1000, '2026-12-31T23:59:59Z', '2026-12-31T23:59:59Z', 'active', 'adm_root', 'seed enterprise redeem code')
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
  ('aet_short_text', 'short_text', 'Short Text', '2026-06-28', '{"type":"object","category":"text","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":10,"render_hint":{"component":"text"}}'::jsonb, 'active'),
  ('aet_long_text', 'long_text', 'Long Text', '2026-06-28', '{"type":"object","category":"text","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":20,"render_hint":{"component":"text"}}'::jsonb, 'active'),
  ('aet_rich_text', 'rich_text', 'Rich Text', '2026-06-28', '{"type":"object","category":"text","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":30,"render_hint":{"component":"rich_text"}}'::jsonb, 'active'),
  ('aet_structured_object', 'structured_object', 'Structured Object', '2026-06-28', '{"type":"object","category":"json","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":40,"render_hint":{"component":"json"}}'::jsonb, 'active'),
  ('aet_list', 'list', 'List', '2026-06-28', '{"type":"object","category":"json","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":50,"render_hint":{"component":"list"}}'::jsonb, 'active'),
  ('aet_image_ref', 'image_ref', 'Image Reference', '2026-06-28', '{"type":"object","category":"image","resource_type":"image","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":60,"render_hint":{"component":"image"}}'::jsonb, 'active'),
  ('aet_audio_ref', 'audio_ref', 'Audio Reference', '2026-06-28', '{"type":"object","category":"audio","resource_type":"music","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":false,"referable":true,"sort_order":70,"render_hint":{"component":"audio_player"}}'::jsonb, 'active'),
  ('aet_video_ref', 'video_ref', 'Video Reference', '2026-06-28', '{"type":"object","category":"video","resource_type":"video","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":false,"referable":true,"sort_order":80,"render_hint":{"component":"video_player"}}'::jsonb, 'active'),
  ('aet_file_ref', 'file_ref', 'File Reference', '2026-06-28', '{"type":"object","category":"file","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":false,"referable":true,"sort_order":90,"render_hint":{"component":"file"}}'::jsonb, 'active'),
  ('aet_prompt', 'prompt', 'Prompt', '2026-06-28', '{"type":"object","category":"prompt","resource_type":"file","usage_stage":"draft","draft_enabled":true,"final_enabled":false,"editable":true,"referable":true,"sort_order":100,"render_hint":{"component":"prompt"}}'::jsonb, 'active'),
  ('aet_storyboard', 'storyboard', 'Storyboard', '2026-06-28', '{"type":"object","category":"storyboard","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":110,"render_hint":{"component":"storyboard"}}'::jsonb, 'active'),
  ('aet_timeline', 'timeline', 'Timeline', '2026-06-28', '{"type":"object","category":"timeline","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":120,"render_hint":{"component":"timeline"}}'::jsonb, 'active'),
  ('aet_tag_group', 'tag_group', 'Tag Group', '2026-06-28', '{"type":"object","category":"metadata","resource_type":"file","usage_stage":"draft_final","draft_enabled":true,"final_enabled":true,"editable":true,"referable":true,"sort_order":130,"render_hint":{"component":"tags"}}'::jsonb, 'active'),
  ('aet_parameter_group', 'parameter_group', 'Parameter Group', '2026-06-28', '{"type":"object","category":"parameters","resource_type":"file","usage_stage":"draft","draft_enabled":true,"final_enabled":false,"editable":true,"referable":true,"sort_order":140,"render_hint":{"component":"parameters"}}'::jsonb, 'active')
ON CONFLICT (element_type) DO NOTHING;

INSERT INTO assets (
  id, asset_no, owner_user_id, space_id, enterprise_id, project_id, asset_type, title, status, visibility, source_type, source_ref_id, content_digest, metadata_json
) VALUES
  ('ast_generated_1001', 'A1001', 'usr_1001', 'sp_personal_1001', null, 'prj_active_1001', 'image', 'Generated seed image', 'active', 'private', 'agent_commit', 'art_1001', 'sha256:artifact-content', '{"width":1024,"height":1024}'::jsonb),
  ('ast_other_space_1002', 'A2001', 'usr_1002', 'sp_personal_1002', null, 'prj_other_space_1002', 'image', 'Cross-space asset', 'active', 'private', 'upload', 'upload_seed_2001', 'sha256:other-space-content', '{}'::jsonb)
ON CONFLICT (asset_no) DO NOTHING;

INSERT INTO project_assets (
  id, project_id, asset_id, asset_role, attached_by_user_id, attached_by, status, source_session_id, source_run_id, source_artifact_id, source_type
) VALUES
  ('pa_generated_1001_cover', 'prj_active_1001', 'ast_generated_1001', 'cover', 'usr_1001', 'agent', 'active', 'sess_seed_1001', 'run_seed_1001', 'art_1001', 'agent_commit'),
  ('pa_other_space_1002_cover', 'prj_other_space_1002', 'ast_other_space_1002', 'cover', 'usr_1002', 'user', 'active', null, null, null, 'upload')
ON CONFLICT (project_id, asset_id, asset_role) DO NOTHING;

INSERT INTO asset_storage_objects (
  id, asset_id, bucket, object_key_digest, object_uri, mime_type, size_bytes, checksum, storage_status, preview_uri
) VALUES (
  'aso_generated_1001', 'ast_generated_1001', 'dora-local', 'sha256:object-key-generated-1001', 'tos://dora-local/private/generated/1001.png', 'image/png', 2048, 'sha256:artifact-content', 'available', 'http://localhost:19080/api/assets/ast_generated_1001/preview'
) ON CONFLICT DO NOTHING;

INSERT INTO asset_elements (
  id, asset_id, element_type, element_key, element_summary_json, preview_text, status
) VALUES (
  'asel_generated_1001_primary', 'ast_generated_1001', 'image_ref', 'primary', '{"width":1024,"height":1024}'::jsonb, null, 'active'
) ON CONFLICT (asset_id, element_key) DO NOTHING;

INSERT INTO works (
  id, work_no, project_id, owner_user_id, space_id, title, description, cover_asset_id, status, latest_snapshot_id,
  share_status, current_snapshot_id, category, tags
) VALUES (
  'wrk_seed_public', 'W1001', 'prj_active_1001', 'usr_1001', 'sp_personal_1001', 'Seed public storyboard', 'A public seed work', 'ast_generated_1001', 'public', 'wps_seed_public',
  'shared', 'wps_seed_public', 'storyboard', '["seed","storyboard"]'::jsonb
) ON CONFLICT (work_no) DO NOTHING;

INSERT INTO work_assets (
  id, work_asset_id, work_id, asset_id, asset_role, role, sort_order, display_order
) VALUES (
  'wka_seed_public_cover', 'wka_seed_public_cover', 'wrk_seed_public', 'ast_generated_1001', 'cover', 'cover', 0, 0
) ON CONFLICT (work_id, asset_id, asset_role) DO NOTHING;

INSERT INTO work_public_snapshots (
  id, snapshot_id, work_id, share_slug, public_work_id, public_slug, title, description, cover_asset_id,
  snapshot_json, snapshot_payload, public_media_refs, share_url, public_url, visibility, status, like_count,
  category, resource_type, published_by_user_id, published_by, published_at
) VALUES (
  'wps_seed_public_row', 'wps_seed_public', 'wrk_seed_public', 'seed-storyboard', 'pubw_seed_storyboard', 'seed-storyboard', 'Seed public storyboard', 'A public seed work', 'ast_generated_1001',
  '{"assets":[{"asset_id":"ast_generated_1001","element_type":"image_ref"}]}'::jsonb,
  '{"public_work_id":"pubw_seed_storyboard","title":"Seed public storyboard","description":"A public seed work","tags":["seed","storyboard"],"share_url":"http://localhost:3000/share/seed-storyboard","public_media_refs":[{"public_media_id":"pm_seed_storyboard","resource_type":"image","variant":"preview","public_media_url":"http://localhost/tos/local/public/works/pubw_seed_storyboard/snapshots/wps_seed_public/media/pm_seed_storyboard/preview"}],"author_display_name":"Dora Creator"}'::jsonb,
  '[{"public_media_id":"pm_seed_storyboard","resource_type":"image","variant":"preview","public_media_url":"http://localhost/tos/local/public/works/pubw_seed_storyboard/snapshots/wps_seed_public/media/pm_seed_storyboard/preview"}]'::jsonb,
  'http://localhost:3000/share/seed-storyboard', 'http://localhost:3000/share/seed-storyboard', 'public', 'active', 1,
  'storyboard', 'image', 'usr_1001', 'usr_1001', '2026-06-27T12:00:00Z'
) ON CONFLICT (snapshot_id) DO NOTHING;

INSERT INTO work_likes (
  id, like_id, snapshot_id, public_work_id, work_id, user_id, status, liked_at
) VALUES (
  'wlike_seed_1002', 'wlike_seed_1002', 'wps_seed_public', 'pubw_seed_storyboard', 'wrk_seed_public', 'usr_1002', 'liked', '2026-06-27T12:05:00Z'
) ON CONFLICT (snapshot_id, user_id) DO NOTHING;

INSERT INTO work_categories (
  id, category_code, category_key, display_name, sort_order, status
) VALUES (
  'wcat_storyboard', 'storyboard', 'storyboard', 'Storyboard', 10, 'active'
) ON CONFLICT (category_code) DO NOTHING;

INSERT INTO notifications (
  id, notification_id, notification_no, recipient_user_id, recipient_space_id, recipient_enterprise_id,
  notification_type, type, title, summary, body, jump_type, jump_target_id, jump_payload_json,
  related_resource_type, related_resource_id, navigation_hint, source_type, source_id, status, idempotency_key, trace_id
) VALUES (
  'ntf_skill_review_001', 'ntf_skill_review_001', 'N1001', 'usr_1001', 'sp_personal_1001', null,
  'skill.review.approved', 'skill_review_approved', 'Skill approved', 'Your storyboard skill has been approved.', 'Your storyboard skill has been approved.', 'skill_version', 'skv_seed_storyboard_100', '{"skill_id":"sk_seed_storyboard"}'::jsonb,
  'skill_version', 'skv_seed_storyboard_100', '{"target_route":"/skills/sk_seed_storyboard","target_resource_id":"sk_seed_storyboard"}'::jsonb, 'skill_review', 'skv_seed_storyboard_100', 'unread', 'seed:ntf_skill_review_001', 'seed-notification'
) ON CONFLICT (notification_no) DO NOTHING;
