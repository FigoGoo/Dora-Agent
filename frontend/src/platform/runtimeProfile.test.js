import { describe, expect, it } from 'vitest';
import {
  MEDIA_RUNTIME_PROFILE,
  MVP_ALL_TOOLS_RUNTIME_PROFILE,
  resolveRuntimeCapabilities,
  runtimeCapabilityEnabled
} from './runtimeProfile.js';

describe('runtimeProfile', () => {
  it('keeps all capabilities closed by default and preserves isolated compatibility', () => {
    expect(resolveRuntimeCapabilities({})).toEqual({
      profile: '', mediaProfile: '', userMessage: false, planCreationSpec: false,
      analyzeMaterials: false, planStoryboard: false, writePrompts: false,
      generateMedia: false, assembleOutput: false
    });
    expect(runtimeCapabilityEnabled('planStoryboard', {
      VITE_DORA_PLAN_STORYBOARD_RUNTIME_ENABLED: 'true'
    })).toBe(true);
  });

  it('derives the exact implemented MVP capability set from one profile', () => {
    expect(resolveRuntimeCapabilities({ VITE_DORA_RUNTIME_PROFILE: MVP_ALL_TOOLS_RUNTIME_PROFILE })).toEqual({
      profile: MVP_ALL_TOOLS_RUNTIME_PROFILE,
      mediaProfile: '',
      userMessage: true,
      planCreationSpec: true,
      analyzeMaterials: true,
      planStoryboard: true,
      writePrompts: true,
      generateMedia: false,
      assembleOutput: false
    });
  });

  it('opens exactly two media capabilities only when the approved dependent profile is also enabled', () => {
    expect(resolveRuntimeCapabilities({
      VITE_DORA_RUNTIME_PROFILE: MVP_ALL_TOOLS_RUNTIME_PROFILE,
      VITE_DORA_MEDIA_RUNTIME_PROFILE: MEDIA_RUNTIME_PROFILE
    })).toMatchObject({
      profile: MVP_ALL_TOOLS_RUNTIME_PROFILE,
      mediaProfile: MEDIA_RUNTIME_PROFILE,
      generateMedia: true,
      assembleOutput: true
    });
    expect(() => resolveRuntimeCapabilities({ VITE_DORA_MEDIA_RUNTIME_PROFILE: MEDIA_RUNTIME_PROFILE }))
      .toThrow('必须依赖');
    expect(() => resolveRuntimeCapabilities({
      VITE_DORA_RUNTIME_PROFILE: MVP_ALL_TOOLS_RUNTIME_PROFILE,
      VITE_DORA_MEDIA_RUNTIME_PROFILE: 'media.runtime.v4'
    })).toThrow('不受支持');
  });

  it('fails closed for unknown profiles, mixed switches, and unknown capabilities', () => {
    expect(() => resolveRuntimeCapabilities({ VITE_DORA_RUNTIME_PROFILE: 'mvp_all_tools.runtime.v2' }))
      .toThrow('不受支持');
    expect(() => resolveRuntimeCapabilities({
      VITE_DORA_RUNTIME_PROFILE: MVP_ALL_TOOLS_RUNTIME_PROFILE,
      VITE_DORA_PLAN_SPEC_PREVIEW_ENABLED: 'true'
    })).toThrow('不能与隔离');
    expect(() => runtimeCapabilityEnabled('unknownMedia', {})).toThrow('未知 Runtime capability');
  });
});
