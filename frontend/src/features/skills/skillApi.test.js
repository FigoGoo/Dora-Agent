import { describe, expect, it, vi } from 'vitest';
import { ownerSkillFixture, ownerSkillResponseFixture, SKILL_IDS, skillDefinitionFixture } from '../../test/skillFixtures.js';
import {
  createOwnerSkill,
  getOwnerSkill,
  listOwnerSkills,
  submitOwnerSkillReview,
  updateOwnerSkillDraft
} from './skillApi.js';

describe('Owner Skill API', () => {
  it('creates a structured draft with CSRF and a stable Idempotency-Key', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(ownerSkillResponseFixture(), 201));
    vi.stubGlobal('fetch', fetchMock);

    await createOwnerSkill({
      definition: skillDefinitionFixture(),
      idempotencyKey: 'skill-create-key',
      csrfToken: 'csrf-1'
    });

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/skills', expect.objectContaining({
      method: 'POST',
      credentials: 'include',
      headers: expect.objectContaining({
        'Idempotency-Key': 'skill-create-key',
        'X-CSRF-Token': 'csrf-1'
      })
    }));
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.definition.public_tool_refs).toEqual([]);
    expect(body.definition.schema_version).toBe('skill_definition.v1');
  });

  it('sorts request collections exactly like Go UTF-8 byte ordering', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(ownerSkillResponseFixture(), 201));
    vi.stubGlobal('fetch', fetchMock);
    const privateUse = '\uE000';
    const emoji = '😀';

    await createOwnerSkill({
      definition: skillDefinitionFixture({
        tags: [emoji, privateUse],
        starter_prompts: [emoji, privateUse],
        examples: [
          { input: emoji, output: '输出' },
          { input: privateUse, output: '输出' }
        ]
      }),
      idempotencyKey: 'skill-create-key',
      csrfToken: 'csrf-1'
    });

    const definition = JSON.parse(fetchMock.mock.calls[0][1].body).definition;
    expect(definition.tags).toEqual([privateUse, emoji]);
    expect(definition.starter_prompts).toEqual([privateUse, emoji]);
    expect(definition.examples.map((item) => item.input)).toEqual([privateUse, emoji]);
  });

  it('rejects forged public_tool_refs before sending a request', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    await expect(createOwnerSkill({
      definition: skillDefinitionFixture({ public_tool_refs: [{ tool_id: 'forged' }] }),
      idempotencyKey: 'skill-create-key',
      csrfToken: 'csrf-1'
    })).rejects.toMatchObject({ code: 'SKILL_TOOL_REFERENCE_FORBIDDEN' });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('uses mine scope, Owner detail, quoted If-Match and review paths', async () => {
    const responses = [
      { items: [ownerSkillFixture()], next_cursor: 'next-1', request_id: SKILL_IDS.request },
      ownerSkillResponseFixture(),
      ownerSkillResponseFixture(),
      {
        skill: ownerSkillFixture({ review_status: 'reviewing' }),
        review_id: SKILL_IDS.review,
        request_id: SKILL_IDS.request
      }
    ];
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse(responses.shift())));
    vi.stubGlobal('fetch', fetchMock);

    await listOwnerSkills({ cursor: 'cursor-1' });
    await getOwnerSkill(SKILL_IDS.skill);
    await updateOwnerSkillDraft({
      skillID: SKILL_IDS.skill,
      definition: skillDefinitionFixture(),
      draftETag: '"draft-etag-1"',
      csrfToken: 'csrf-1'
    });
    await submitOwnerSkillReview({
      skillID: SKILL_IDS.skill,
      idempotencyKey: 'skill-review-key',
      draftETag: '"draft-etag-1"',
      csrfToken: 'csrf-1'
    });

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/skills?scope=mine&cursor=cursor-1');
    expect(fetchMock.mock.calls[1][0]).toBe(`/api/v1/skills/${SKILL_IDS.skill}`);
    expect(fetchMock.mock.calls[2][0]).toBe(`/api/v1/skills/${SKILL_IDS.skill}/draft`);
    expect(fetchMock.mock.calls[2][1].headers['If-Match']).toBe('"draft-etag-1"');
    expect(fetchMock.mock.calls[3][0]).toBe(`/api/v1/skills/${SKILL_IDS.skill}/reviews`);
    expect(fetchMock.mock.calls[3][1].headers['Idempotency-Key']).toBe('skill-review-key');
    expect(fetchMock.mock.calls[3][1].headers['If-Match']).toBe('"draft-etag-1"');
  });

  it('requires CSRF, Idempotency-Key and If-Match before writes', async () => {
    await expect(createOwnerSkill({ definition: skillDefinitionFixture(), idempotencyKey: 'key' })).rejects.toThrow('CSRF');
    await expect(createOwnerSkill({ definition: skillDefinitionFixture(), csrfToken: 'csrf' })).rejects.toThrow('Idempotency');
    await expect(updateOwnerSkillDraft({
      skillID: SKILL_IDS.skill, definition: skillDefinitionFixture(), csrfToken: 'csrf'
    })).rejects.toThrow('draft_etag');
    await expect(submitOwnerSkillReview({
      skillID: SKILL_IDS.skill, idempotencyKey: 'review-key', csrfToken: 'csrf'
    })).rejects.toThrow('draft_etag');
  });

  it('surfaces Owner API 401 through the shared auth-expiry path', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      error: { code: 'UNAUTHENTICATED', message: '会话已过期', retryable: false }
    }, 401)));

    await expect(listOwnerSkills()).rejects.toMatchObject({
      status: 401,
      code: 'UNAUTHENTICATED',
      message: '会话已过期'
    });
  });
});

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } });
}
