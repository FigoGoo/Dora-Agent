import { describe, expect, it } from 'vitest';
import { directResponseCardFixture, turnFailureCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import {
  parseDirectResponseCard,
  parseTurnFailureCard,
  parseTurnOutputProjection,
  TurnOutputContractError
} from './turnOutputContract.js';

describe('Turn Output frozen contract', () => {
  it('strictly parses the fixed Direct Response Card', () => {
    expect(parseDirectResponseCard(directResponseCardFixture())).toEqual({
      kind: 'direct_response',
      schemaVersion: 'session.turn.direct_response.card.v1',
      turnID: WORKSPACE_IDS.turn,
      runID: WORKSPACE_IDS.run,
      inputID: WORKSPACE_IDS.input,
      status: 'completed',
      messageCode: 'creation_request_received',
      summary: '已收到你的创作需求。你可以继续打开工具箱选择下一步流程。',
      availableActions: ['open_toolbox']
    });
  });

  it.each([
    ['unknown action', { available_actions: ['run_tool'] }],
    ['unknown status', { status: 'resolved' }],
    ['summary drift', { summary: '模型生成的任意文案' }],
    ['extra field', { provider_payload: 'secret' }],
    ['unknown schema', { schema_version: 'session.turn.direct_response.card.v2' }]
  ])('fails closed on Direct Response %s', (_name, overrides) => {
    expect(() => parseTurnOutputProjection(directResponseCardFixture(overrides))).toThrow(TurnOutputContractError);
  });

  it.each(['failed', 'recovery_pending'])('strictly parses Failure status %s', (status) => {
    const parsed = parseTurnFailureCard(turnFailureCardFixture({ status }));
    expect(parsed).toMatchObject({ kind: 'failure', status, inputID: WORKSPACE_IDS.input });
  });

  it.each([
    ['unknown status', { status: 'completed' }],
    ['unsafe error code', { error_code: 'provider raw error' }],
    ['non boolean retryable', { retryable: 'yes' }],
    ['empty summary', { summary: '' }],
    ['unpaired surrogate', { summary: '\ud800' }],
    ['extra field', { stack: 'secret' }]
  ])('fails closed on Failure Card %s', (_name, overrides) => {
    expect(() => parseTurnFailureCard(turnFailureCardFixture(overrides))).toThrow(TurnOutputContractError);
  });

  it('maps explicit null to no output', () => {
    expect(parseTurnOutputProjection(null)).toBeNull();
  });
});
