import { describe, expect, it } from 'vitest';
import {
  canAccessPage,
  getPageFromPath,
  getPathForPage,
  getSkillReviewDetailPath,
  matchOwnerSkillBuilderPath,
  matchPublicSkillDetailPath,
  matchSkillReviewDetailPath,
  PUBLIC_PAGES
} from './routes.js';
import { SKILL_IDS } from '../test/skillFixtures.js';

describe('Skill routes', () => {
  it('keeps the public market canonical at /skills and recognizes /skill as legacy', () => {
    expect(getPathForPage('skills')).toBe('/skills');
    expect(getPageFromPath('/skills')).toBe('skills');
    expect(getPageFromPath('/skill')).toBe('skills');
    expect(getPageFromPath('/skills/public-skill-id')).toBe('skillDetail');
    expect(PUBLIC_PAGES.has('skills')).toBe(true);
    expect(PUBLIC_PAGES.has('skillDetail')).toBe(true);
  });

  it('matches only an exact canonical lowercase UUIDv7 public detail path', () => {
    const detailPath = `/skills/${SKILL_IDS.skill}`;
    expect(getPageFromPath(detailPath)).toBe('skillDetail');
    expect(matchPublicSkillDetailPath(detailPath)).toEqual({ skillID: SKILL_IDS.skill });
    expect(matchPublicSkillDetailPath('/skills/not-a-uuid')).toBeNull();
    expect(matchPublicSkillDetailPath(`/skills/${SKILL_IDS.skill.toUpperCase()}`)).toBeNull();
    expect(matchPublicSkillDetailPath(`${detailPath}/`)).toBeNull();
    expect(getPageFromPath(`${detailPath}/extra`)).toBe('skillDetail');
    expect(matchPublicSkillDetailPath(`${detailPath}/extra`)).toBeNull();
    expect(matchPublicSkillDetailPath(`/skills/${SKILL_IDS.skill.replace(/^0/, '%30')}`)).toBeNull();
  });

  it('matches protected Owner Skill list, create and edit routes', () => {
    expect(getPageFromPath('/my/skills')).toBe('mySkills');
    expect(getPageFromPath('/my/skills/new')).toBe('skillBuilder');
    expect(getPageFromPath(`/my/skills/${SKILL_IDS.skill}/edit`)).toBe('skillBuilder');
    expect(matchOwnerSkillBuilderPath('/my/skills/new')).toEqual({ skillID: '' });
    expect(matchOwnerSkillBuilderPath(`/my/skills/${SKILL_IDS.skill}/edit`)).toEqual({ skillID: SKILL_IDS.skill });
    expect(getPageFromPath('/my/skills/not-a-uuid/edit')).toBe('skillBuilder');
    expect(matchOwnerSkillBuilderPath('/my/skills/not-a-uuid/edit')).toBeNull();
    expect(getPageFromPath('/my/skills//edit')).toBe('skillBuilder');
    expect(matchOwnerSkillBuilderPath('/my/skills//edit')).toBeNull();
    expect(matchOwnerSkillBuilderPath('/my/skills/%E0%A4%A/edit')).toBeNull();
    expect(PUBLIC_PAGES.has('mySkills')).toBe(false);
    expect(PUBLIC_PAGES.has('skillBuilder')).toBe(false);
  });

  it('accepts only the exact Reviewer queue and canonical lowercase UUIDv7 detail', () => {
    const detailPath = `/admin/skills/reviews/${SKILL_IDS.review}`;
    expect(getPageFromPath('/admin/skills/reviews')).toBe('skillReviews');
    expect(getPathForPage('skillReviews')).toBe('/admin/skills/reviews');
    expect(getPageFromPath(detailPath)).toBe('skillReviewDetail');
    expect(matchSkillReviewDetailPath(detailPath)).toEqual({ reviewID: SKILL_IDS.review });
    expect(getSkillReviewDetailPath(SKILL_IDS.review)).toBe(detailPath);

    expect(getPageFromPath('/admin/skills/reviews/')).toBe('skillReviewDetail');
    expect(matchSkillReviewDetailPath('/admin/skills/reviews/')).toBeNull();
    expect(matchSkillReviewDetailPath(`${detailPath}/`)).toBeNull();
    expect(matchSkillReviewDetailPath(`/admin/skills/reviews/${SKILL_IDS.review.toUpperCase()}`)).toBeNull();
    expect(matchSkillReviewDetailPath('/admin/skills/reviews/not-a-uuid')).toBeNull();
    expect(matchSkillReviewDetailPath(`/admin/skills/reviews/${SKILL_IDS.review.replace(/^0/, '%30')}`)).toBeNull();
    expect(() => getSkillReviewDetailPath('not-a-uuid')).toThrow('规范 UUIDv7');
    expect(PUBLIC_PAGES.has('skillReviews')).toBe(false);
    expect(PUBLIC_PAGES.has('skillReviewDetail')).toBe(false);
  });

  it('grants Reviewer pages from the exact capability, never from role names', () => {
    expect(canAccessPage('skillReviews', ['skill.review'])).toBe(true);
    expect(canAccessPage('skillReviewDetail', ['project.read', 'skill.review'])).toBe(true);
    expect(canAccessPage('skillReviews', ['skill.review'], ['skill.review'])).toBe(false);
    expect(canAccessPage('skillReviews', ['admin'])).toBe(false);
    expect(canAccessPage('skillReviews', null)).toBe(false);
    expect(canAccessPage('home', [])).toBe(true);
  });
});
