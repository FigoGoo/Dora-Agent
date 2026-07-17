export const MVP_ALL_TOOLS_RUNTIME_PROFILE = 'mvp_all_tools.runtime.v1preview1';
export const MEDIA_RUNTIME_PROFILE = 'media.runtime.v3preview1';

const LEGACY_CAPABILITY_ENV = Object.freeze({
  planCreationSpec: 'VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED',
  analyzeMaterials: 'VITE_DORA_ANALYZE_MATERIALS_RUNTIME_ENABLED',
  planStoryboard: 'VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED',
  writePrompts: 'VITE_DORA_WRITE_PROMPTS_RUNTIME_ENABLED'
});

// resolveRuntimeCapabilities 让一个精确 Profile 成为本地 MVP 的能力真相；未知值和混用旧开关失败关闭。
export function resolveRuntimeCapabilities(env = import.meta.env) {
  const profile = String(env.VITE_DORA_RUNTIME_PROFILE ?? '').trim();
  const mediaProfile = String(env.VITE_DORA_MEDIA_RUNTIME_PROFILE ?? '').trim();
  const legacy = Object.fromEntries(
    Object.entries(LEGACY_CAPABILITY_ENV).map(([capability, key]) => [capability, env[key] === 'true'])
  );

  if (profile === '') {
    if (mediaProfile !== '') throw new Error('媒体 Runtime Profile 必须依赖统一 MVP Profile');
    return Object.freeze({
      profile, mediaProfile, userMessage: false, ...legacy,
      generateMedia: false, assembleOutput: false
    });
  }
  if (profile !== MVP_ALL_TOOLS_RUNTIME_PROFILE) {
    throw new Error('VITE_DORA_RUNTIME_PROFILE 不受支持');
  }
  if (Object.values(legacy).some(Boolean)) {
    throw new Error('统一 Runtime Profile 不能与隔离 Preview 开关同时启用');
  }
  if (mediaProfile !== '' && mediaProfile !== MEDIA_RUNTIME_PROFILE) {
    throw new Error('VITE_DORA_MEDIA_RUNTIME_PROFILE 不受支持');
  }
  return Object.freeze({
    profile,
    mediaProfile,
    userMessage: true,
    planCreationSpec: true,
    analyzeMaterials: true,
    planStoryboard: true,
    writePrompts: true,
    generateMedia: mediaProfile === MEDIA_RUNTIME_PROFILE,
    assembleOutput: mediaProfile === MEDIA_RUNTIME_PROFILE
  });
}

export function runtimeCapabilityEnabled(capability, env = import.meta.env) {
  const capabilities = resolveRuntimeCapabilities(env);
  if (!Object.prototype.hasOwnProperty.call(capabilities, capability) || capability === 'profile' || capability === 'mediaProfile') {
    throw new Error(`未知 Runtime capability: ${capability}`);
  }
  return capabilities[capability];
}
