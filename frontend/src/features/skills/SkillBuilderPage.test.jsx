import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ownerSkillFixture, SKILL_IDS } from '../../test/skillFixtures.js';
import { parseOwnerSkill, SKILL_CAPABILITY_FIELDS, SKILL_CAPABILITY_LABELS } from './skillContract.js';
import { SkillBuilderPage } from './SkillBuilderPage.jsx';
import { createSkillCommandLedger } from './skillCommandLedger.jsx';

describe('SkillBuilderPage', () => {
  it('clears a field validation error when that field is corrected', async () => {
    const user = userEvent.setup();
    const client = { create: vi.fn(), get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };

    render(<SkillBuilderPage csrfToken="csrf-1" client={client} />);
    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    expect(screen.getByRole('alert')).toHaveTextContent('definition.name 不能为空');
    const nameInput = screen.getByLabelText(/Skill 名称/);
    expect(nameInput).toBeRequired();
    expect(nameInput).toHaveAttribute('aria-invalid', 'true');
    expect(document.getElementById(nameInput.getAttribute('aria-describedby'))).toHaveTextContent('definition.name 不能为空');

    await user.type(nameInput, '已修正名称');
    expect(screen.queryByText('definition.name 不能为空')).not.toBeInTheDocument();
    expect(client.create).not.toHaveBeenCalled();
  });

  it('submits six independent capability fields and fixes public_tool_refs to empty', async () => {
    const user = userEvent.setup();
    const onNavigate = vi.fn();
    const commandLedger = createSkillCommandLedger();
    const create = vi.fn().mockImplementation(async ({ definition }) => ({
      skill: parseOwnerSkill(ownerSkillFixture({ definition }))
    }));
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };

    render(
      <SkillBuilderPage
        csrfToken="csrf-1"
        client={client}
        commandLedger={commandLedger}
        onNavigate={onNavigate}
      />
    );

    expect(document.querySelectorAll('.skill-capability-field')).toHaveLength(SKILL_CAPABILITY_FIELDS.length);
    expect(screen.getByLabelText('公共 Tool 引用状态')).toHaveTextContent('提交值固定为空集合');
    expect(screen.queryByRole('textbox', { name: /公共 Tool/ })).not.toBeInTheDocument();

    await user.type(screen.getByLabelText(/Skill 名称/), '独立能力 Skill');
    for (const field of SKILL_CAPABILITY_FIELDS) {
      const label = SKILL_CAPABILITY_LABELS[field];
      if (field === 'write_prompts') {
        await user.selectOptions(screen.getByLabelText(`${label}适用性`), 'not_applicable');
        await user.type(screen.getByLabelText(`${label}不适用原因`), '此流程不生成提示词');
      } else {
        await user.type(screen.getByLabelText(`${label}业务指导`), `${field} 独立指导`);
      }
    }
    await user.click(screen.getByRole('button', { name: '创建草稿' }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    const command = create.mock.calls[0][0];
    expect(command.csrfToken).toBe('csrf-1');
    expect(command.signal).toBeInstanceOf(AbortSignal);
    expect(command.idempotencyKey).toMatch(/^skill-create-/);
    expect(command.definition.public_tool_refs).toEqual([]);
    expect(command.definition.write_prompts).toEqual({
      applicability: 'not_applicable',
      guidance: '',
      not_applicable_reason: '此流程不生成提示词'
    });
    expect(new Set(SKILL_CAPABILITY_FIELDS.map((field) => (
      command.definition[field].guidance || command.definition[field].not_applicable_reason
    ))).size).toBe(SKILL_CAPABILITY_FIELDS.length);
    expect(onNavigate).toHaveBeenCalledWith(`/my/skills/${SKILL_IDS.skill}/edit`);
    expect(commandLedger.get('skill:create')).toBeNull();
  });

  it('uses draft_etag and preserves local form values after a 409 conflict', async () => {
    const user = userEvent.setup();
    const existing = parseOwnerSkill(ownerSkillFixture());
    const conflict = Object.assign(new Error('草稿已被其他请求更新'), { status: 409, code: 'SKILL_DRAFT_CONFLICT' });
    const update = vi.fn().mockRejectedValue(conflict);
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update,
      submitReview: vi.fn()
    };

    render(<SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />);

    const nameInput = await screen.findByLabelText(/Skill 名称/);
    expect(client.get).toHaveBeenCalledWith(SKILL_IDS.skill, { signal: expect.any(AbortSignal) });
    await user.clear(nameInput);
    await user.type(nameInput, '本地未保存的新名称');
    await user.click(screen.getByRole('button', { name: '保存草稿' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('本地表单已保留');
    expect(screen.getByLabelText(/Skill 名称/)).toHaveValue('本地未保存的新名称');
    expect(update).toHaveBeenCalledWith(expect.objectContaining({
      skillID: SKILL_IDS.skill,
      draftETag: '"draft-etag-1"',
      csrfToken: 'csrf-1',
      signal: expect.any(AbortSignal)
    }));
  });

  it('maps Business bare field paths to controls with accessible errors', async () => {
    const user = userEvent.setup();
    const commandLedger = createSkillCommandLedger();
    const validation = Object.assign(new Error('Skill 定义不合法'), {
      status: 422,
      code: 'SKILL_INVALID_DEFINITION',
      details: [
        { field: 'name', code: 'REQUIRED', message: '名称已被服务端拒绝' },
        { field: 'plan_creation_spec.guidance', code: 'REQUIRED', message: '请填写流程规划指导' }
      ]
    });
    const create = vi.fn().mockRejectedValue(validation);
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };
    render(<SkillBuilderPage csrfToken="csrf-1" client={client} commandLedger={commandLedger} />);
    await fillMinimumDefinition(user, '字段映射 Skill');

    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    expect(await screen.findByText('名称已被服务端拒绝')).toBeInTheDocument();
    expect(screen.getByText('请填写流程规划指导')).toBeInTheDocument();

    const nameInput = screen.getByLabelText(/Skill 名称/);
    const guidance = screen.getByLabelText('流程规划业务指导');
    expect(nameInput).toHaveAttribute('aria-invalid', 'true');
    expect(guidance).toHaveAttribute('aria-invalid', 'true');
    expect(document.getElementById(nameInput.getAttribute('aria-describedby'))).toHaveTextContent('名称已被服务端拒绝');
    expect(document.getElementById(guidance.getAttribute('aria-describedby'))).toHaveTextContent('请填写流程规划指导');
    expect(commandLedger.get('skill:create')).toBeNull();

    await user.type(guidance, '补充');
    expect(screen.queryByText('请填写流程规划指导')).not.toBeInTheDocument();
  });

  it('maps normalized example index errors to the examples collection instead of a wrong control', async () => {
    const user = userEvent.setup();
    const validation = Object.assign(new Error('Skill 定义不合法'), {
      status: 400,
      code: 'SKILL_INVALID_DEFINITION',
      details: {
        field_errors: [{
          field: 'definition.examples[1].input',
          code: 'TOO_LONG',
          message: '示例输入超过长度限制'
        }]
      }
    });
    const client = {
      create: vi.fn().mockRejectedValue(validation),
      get: vi.fn(),
      update: vi.fn(),
      submitReview: vi.fn()
    };
    render(<SkillBuilderPage csrfToken="csrf-1" client={client} />);
    await fillMinimumDefinition(user, '示例索引 Skill');
    await user.click(screen.getByRole('button', { name: '添加示例' }));
    await user.type(screen.getByLabelText(/^示例 1 输入/), 'B 输入');
    await user.type(screen.getByLabelText(/^示例 1 输出/), 'B 输出');
    await user.click(screen.getByRole('button', { name: '添加示例' }));
    await user.type(screen.getByLabelText(/^示例 2 输入/), 'A 输入');
    await user.type(screen.getByLabelText(/^示例 2 输出/), 'A 输出');

    await user.click(screen.getByRole('button', { name: '创建草稿' }));

    expect(await screen.findByText('示例输入超过长度限制')).toBeInTheDocument();
    expect(screen.getByLabelText(/^示例 1 输入/)).toHaveAttribute('aria-invalid', 'false');
    expect(screen.getByLabelText(/^示例 2 输入/)).toHaveAttribute('aria-invalid', 'false');
  });

  it('edits, adds and removes every examples entry without hiding retained values', async () => {
    const user = userEvent.setup();
    const definition = ownerSkillFixture().definition;
    definition.examples = [
      { input: 'A 输入', output: 'A 输出' },
      { input: 'B 输入', output: 'B 输出' }
    ];
    const existing = parseOwnerSkill(ownerSkillFixture({ definition }));
    const update = vi.fn().mockImplementation(async ({ definition: submitted }) => ({
      skill: parseOwnerSkill(ownerSkillFixture({ definition: submitted, draft_etag: '"draft-etag-2"' }))
    }));
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update,
      submitReview: vi.fn()
    };
    render(<SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />);

    expect(await screen.findByLabelText(/^示例 1 输入/)).toHaveValue('A 输入');
    expect(screen.getByLabelText(/^示例 2 输入/)).toHaveValue('B 输入');
    expect(screen.getByLabelText(/^示例 1 输入/)).toBeRequired();
    await user.clear(screen.getByLabelText(/^示例 2 输出/));
    await user.type(screen.getByLabelText(/^示例 2 输出/), 'B 新输出');
    await user.click(screen.getByRole('button', { name: '删除示例 1' }));
    expect(screen.getByLabelText(/^示例 1 输入/)).toHaveValue('B 输入');
    await user.click(screen.getByRole('button', { name: '添加示例' }));
    await user.type(screen.getByLabelText(/^示例 2 输入/), 'C 输入');
    await user.type(screen.getByLabelText(/^示例 2 输出/), 'C 输出');
    await user.click(screen.getByRole('button', { name: '保存草稿' }));

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update.mock.calls[0][0].definition.examples).toEqual([
      { input: 'B 输入', output: 'B 新输出' },
      { input: 'C 输入', output: 'C 输出' }
    ]);
  });

  it('only allows review submission for a saved draft with backend permission', async () => {
    const user = userEvent.setup();
    const commandLedger = createSkillCommandLedger();
    const existing = parseOwnerSkill(ownerSkillFixture());
    const reviewing = parseOwnerSkill(ownerSkillFixture({
      review_status: 'reviewing',
      review_updated_at: '2026-07-14T10:00:00+08:00',
      allowed_actions: ['edit_draft']
    }));
    const submitReview = vi.fn().mockResolvedValue({ skill: reviewing, reviewID: SKILL_IDS.review });
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update: vi.fn(),
      submitReview
    };

    render(
      <SkillBuilderPage
        skillID={SKILL_IDS.skill}
        csrfToken="csrf-1"
        client={client}
        commandLedger={commandLedger}
      />
    );

    const submitButton = await screen.findByRole('button', { name: '提交审核' });
    expect(submitButton).toBeEnabled();
    await user.click(submitButton);

    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(1));
    expect(submitReview).toHaveBeenCalledWith(expect.objectContaining({
      skillID: SKILL_IDS.skill,
      draftETag: '"draft-etag-1"',
      csrfToken: 'csrf-1',
      idempotencyKey: expect.stringMatching(/^skill-review-/),
      signal: expect.any(AbortSignal)
    }));
    expect(await screen.findByText('已提交审核，后续编辑不会改变本次审核内容。')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '提交审核' })).toBeDisabled();
    expect(commandLedger.get(`skill:review:${SKILL_IDS.skill}`)).toBeNull();
  });

  it('reuses the create key only for the first normalized semantic after an unknown outcome', async () => {
    const user = userEvent.setup();
    const unknown = new Error('连接在结果返回前断开');
    const create = vi.fn().mockRejectedValue(unknown);
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };
    render(<SkillBuilderPage csrfToken="csrf-1" client={client} />);
    await fillMinimumDefinition(user, '冻结创建 Skill');

    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    const firstKey = create.mock.calls[0][0].idempotencyKey;

    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[1][0].idempotencyKey).toBe(firstKey);
    expect(create.mock.calls[1][0].definition).toEqual(create.mock.calls[0][0].definition);

    const nameInput = screen.getByLabelText(/Skill 名称/);
    await user.clear(nameInput);
    await user.type(nameInput, '改变后的创建语义');
    await user.click(screen.getByRole('button', { name: '创建草稿' }));

    expect(create).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('alert')).toHaveTextContent('幂等 Key 已与首次规范化内容绑定');
  });

  it('keeps the create key and normalized semantic across Builder unmount and remount', async () => {
    const firstUser = userEvent.setup();
    const commandLedger = createSkillCommandLedger();
    const unknown = new Error('连接在结果返回前断开');
    const create = vi.fn().mockRejectedValue(unknown);
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };
    const firstRender = render(
      <SkillBuilderPage csrfToken="csrf-1" client={client} commandLedger={commandLedger} />
    );
    await fillMinimumDefinition(firstUser, '跨路由创建 Skill');
    await firstUser.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    const firstKey = create.mock.calls[0][0].idempotencyKey;
    const firstDefinition = create.mock.calls[0][0].definition;
    firstRender.unmount();

    const secondUser = userEvent.setup();
    render(<SkillBuilderPage csrfToken="csrf-1" client={client} commandLedger={commandLedger} />);
    await fillMinimumDefinition(secondUser, '跨路由创建 Skill');
    await secondUser.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));

    expect(create.mock.calls[1][0].idempotencyKey).toBe(firstKey);
    expect(create.mock.calls[1][0].definition).toEqual(firstDefinition);
  });

  it('distinguishes idempotency conflicts without minting a new create key', async () => {
    const user = userEvent.setup();
    const conflict = Object.assign(new Error('conflict'), { status: 409, code: 'IDEMPOTENCY_CONFLICT' });
    const create = vi.fn().mockRejectedValue(conflict);
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };
    render(<SkillBuilderPage csrfToken="csrf-1" client={client} />);
    await fillMinimumDefinition(user, '幂等冲突 Skill');

    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('不会自动换 Key 重发');
    const firstKey = create.mock.calls[0][0].idempotencyKey;
    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[1][0].idempotencyKey).toBe(firstKey);
  });

  it('binds a review key to the first draft_etag and blocks a changed draft after unknown results', async () => {
    const user = userEvent.setup();
    const existing = parseOwnerSkill(ownerSkillFixture());
    const unknown = new Error('提审结果未知');
    const submitReview = vi.fn().mockRejectedValue(unknown);
    const update = vi.fn().mockImplementation(async ({ definition }) => ({
      skill: parseOwnerSkill(ownerSkillFixture({ definition, draft_etag: '"draft-etag-2"' }))
    }));
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update,
      submitReview
    };
    render(<SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />);

    const submitButton = await screen.findByRole('button', { name: '提交审核' });
    await user.click(submitButton);
    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(1));
    const firstKey = submitReview.mock.calls[0][0].idempotencyKey;
    expect(submitReview.mock.calls[0][0].draftETag).toBe('"draft-etag-1"');
    await user.click(screen.getByRole('button', { name: '提交审核' }));
    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(2));
    expect(submitReview.mock.calls[1][0].idempotencyKey).toBe(firstKey);

    const nameInput = screen.getByLabelText(/Skill 名称/);
    await user.clear(nameInput);
    await user.type(nameInput, '新草稿语义');
    await user.click(screen.getByRole('button', { name: '保存草稿' }));
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: '提交审核' }));

    expect(submitReview).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('alert')).toHaveTextContent('首次 draft_etag 绑定');
  });

  it('keeps the review key and draft_etag across Builder unmount and remount', async () => {
    const commandLedger = createSkillCommandLedger();
    const existing = parseOwnerSkill(ownerSkillFixture());
    const unknown = new Error('提审结果未知');
    const submitReview = vi.fn().mockRejectedValue(unknown);
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update: vi.fn(),
      submitReview
    };
    const firstUser = userEvent.setup();
    const firstRender = render(
      <SkillBuilderPage
        skillID={SKILL_IDS.skill}
        csrfToken="csrf-1"
        client={client}
        commandLedger={commandLedger}
      />
    );
    await firstUser.click(await screen.findByRole('button', { name: '提交审核' }));
    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(1));
    const firstCommand = submitReview.mock.calls[0][0];
    firstRender.unmount();

    const secondUser = userEvent.setup();
    render(
      <SkillBuilderPage
        skillID={SKILL_IDS.skill}
        csrfToken="csrf-1"
        client={client}
        commandLedger={commandLedger}
      />
    );
    await secondUser.click(await screen.findByRole('button', { name: '提交审核' }));
    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(2));

    expect(submitReview.mock.calls[1][0].idempotencyKey).toBe(firstCommand.idempotencyKey);
    expect(submitReview.mock.calls[1][0].draftETag).toBe(firstCommand.draftETag);
  });

  it('clears a pending review command when detail reload confirms reviewing on the bound draft', async () => {
    const commandLedger = createSkillCommandLedger();
    commandLedger.set(`skill:review:${SKILL_IDS.skill}`, {
      key: 'review-key-unknown',
      semantic: `${SKILL_IDS.skill}\u0000"draft-etag-1"`,
      draftETag: '"draft-etag-1"'
    });
    const reviewing = parseOwnerSkill(ownerSkillFixture({ review_status: 'reviewing' }));
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: reviewing }),
      update: vi.fn(),
      submitReview: vi.fn()
    };

    render(
      <SkillBuilderPage
        skillID={SKILL_IDS.skill}
        csrfToken="csrf-1"
        client={client}
        commandLedger={commandLedger}
      />
    );

    expect(await screen.findByRole('button', { name: '提交审核' })).toBeDisabled();
    expect(commandLedger.get(`skill:review:${SKILL_IDS.skill}`)).toBeNull();
  });

  it('maps review conflicts separately from draft and idempotency conflicts', async () => {
    const user = userEvent.setup();
    const conflict = Object.assign(new Error('review conflict'), {
      status: 409,
      code: 'SKILL_REVIEW_CONFLICT'
    });
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: parseOwnerSkill(ownerSkillFixture()) }),
      update: vi.fn(),
      submitReview: vi.fn().mockRejectedValue(conflict)
    };
    render(<SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />);

    await user.click(await screen.findByRole('button', { name: '提交审核' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('审核状态已发生变化');
    expect(screen.getByRole('alert')).not.toHaveTextContent('草稿已在其他位置更新');
  });

  it('aborts create and ignores its late resolution after leaving the route', async () => {
    const user = userEvent.setup();
    const onNavigate = vi.fn();
    let resolveCreate;
    const create = vi.fn(({ definition }) => new Promise((resolve) => {
      resolveCreate = () => resolve({ skill: parseOwnerSkill(ownerSkillFixture({ definition })) });
    }));
    const client = { create, get: vi.fn(), update: vi.fn(), submitReview: vi.fn() };

    const { unmount } = render(
      <SkillBuilderPage csrfToken="csrf-1" client={client} onNavigate={onNavigate} />
    );
    await user.type(screen.getByLabelText(/Skill 名称/), '迟到创建 Skill');
    for (const field of SKILL_CAPABILITY_FIELDS) {
      await user.type(
        screen.getByLabelText(`${SKILL_CAPABILITY_LABELS[field]}业务指导`),
        `${field} guidance`
      );
    }
    await user.click(screen.getByRole('button', { name: '创建草稿' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));

    const signal = create.mock.calls[0][0].signal;
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveCreate();
      await Promise.resolve();
    });
    expect(onNavigate).not.toHaveBeenCalled();
  });

  it('aborts an in-flight detail load after leaving the edit route', async () => {
    let resolveDetail;
    let requestSignal;
    const get = vi.fn((skillID, { signal }) => {
      requestSignal = signal;
      return new Promise((resolve) => { resolveDetail = resolve; });
    });
    const client = { create: vi.fn(), get, update: vi.fn(), submitReview: vi.fn() };

    const { unmount } = render(
      <SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />
    );
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    unmount();

    expect(requestSignal.aborted).toBe(true);
    await act(async () => {
      resolveDetail({ skill: parseOwnerSkill(ownerSkillFixture()) });
      await Promise.resolve();
    });
  });

  it('aborts update and ignores its late response after leaving the edit route', async () => {
    const user = userEvent.setup();
    const existing = parseOwnerSkill(ownerSkillFixture());
    let resolveUpdate;
    const update = vi.fn(({ definition }) => new Promise((resolve) => {
      resolveUpdate = () => resolve({
        skill: parseOwnerSkill(ownerSkillFixture({ definition, draft_etag: '"draft-etag-2"' }))
      });
    }));
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update,
      submitReview: vi.fn()
    };
    const { unmount } = render(
      <SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />
    );

    const nameInput = await screen.findByLabelText(/Skill 名称/);
    await user.clear(nameInput);
    await user.type(nameInput, '迟到更新');
    await user.click(screen.getByRole('button', { name: '保存草稿' }));
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    const signal = update.mock.calls[0][0].signal;
    unmount();

    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveUpdate();
      await Promise.resolve();
    });
  });

  it('aborts review and ignores its late response after leaving the edit route', async () => {
    const user = userEvent.setup();
    const existing = parseOwnerSkill(ownerSkillFixture());
    let resolveReview;
    const submitReview = vi.fn(() => new Promise((resolve) => {
      resolveReview = () => resolve({
        skill: parseOwnerSkill(ownerSkillFixture({ review_status: 'reviewing' })),
        reviewID: SKILL_IDS.review
      });
    }));
    const client = {
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({ skill: existing }),
      update: vi.fn(),
      submitReview
    };
    const { unmount } = render(
      <SkillBuilderPage skillID={SKILL_IDS.skill} csrfToken="csrf-1" client={client} />
    );

    await user.click(await screen.findByRole('button', { name: '提交审核' }));
    await waitFor(() => expect(submitReview).toHaveBeenCalledTimes(1));
    const signal = submitReview.mock.calls[0][0].signal;
    unmount();

    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveReview();
      await Promise.resolve();
    });
  });
});

async function fillMinimumDefinition(user, name) {
  await user.type(screen.getByLabelText(/Skill 名称/), name);
  for (const field of SKILL_CAPABILITY_FIELDS) {
    await user.type(
      screen.getByLabelText(`${SKILL_CAPABILITY_LABELS[field]}业务指导`),
      `${field} guidance`
    );
  }
}
