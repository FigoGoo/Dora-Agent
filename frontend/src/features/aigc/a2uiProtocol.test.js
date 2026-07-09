import { describe, expect, it } from 'vitest';
import {
  A2UI_ACTIONS,
  A2UI_COMPONENTS,
  A2UICard,
  A2UI_VERSION,
  InfoCollectionCard,
  a2uiComponent,
  card,
  column,
  componentPayload,
  createInfoCollectionCard,
  fileUpload,
  imagePreview,
  markdown,
  multiChoice,
  row,
  singleChoice,
  text,
  textInput,
  verticalSteps,
  videoPreview
} from './a2uiProtocol.js';

describe('A2UI protocol helpers', () => {
  it('defines the strict protocol version, action names, and component names', () => {
    expect(A2UI_VERSION).toBe('1.0');
    expect(A2UI_ACTIONS).toEqual({
      APPEND_CARD: 'append_card',
      UPDATE_CARD: 'update_card'
    });
    expect(Object.values(A2UI_COMPONENTS)).toEqual([
      'Text',
      'Markdown',
      'Column',
      'Row',
      'Card',
      'TextInput',
      'SingleChoice',
      'MultiChoice',
      'FileUpload',
      'ImagePreview',
      'VideoPreview',
      'VerticalSteps'
    ]);
  });

  it('builds all backend-defined component payloads with standard component names', () => {
    expect(text('title', { value: '标题', usageHint: 'title' })).toEqual({
      id: 'title',
      component: { Text: { value: '标题', usageHint: 'title' } }
    });
    expect(markdown('body', { dataKey: 'answer' })).toEqual({
      id: 'body',
      component: { Markdown: { dataKey: 'answer' } }
    });
    expect(column('col', ['a', 'b'])).toEqual({ id: 'col', component: { Column: { children: ['a', 'b'] } } });
    expect(row('row', ['a', 'b'])).toEqual({ id: 'row', component: { Row: { children: ['a', 'b'] } } });
    expect(card('root', ['body'])).toEqual({ id: 'root', component: { Card: { children: ['body'] } } });
    expect(textInput('name', { key: 'product_name', label: '产品名称', required: true })).toEqual({
      id: 'name',
      component: { TextInput: { key: 'product_name', label: '产品名称', required: true } }
    });
    expect(singleChoice('style', { key: 'style', options: [{ value: 'tech', label: '科技' }] })).toEqual({
      id: 'style',
      component: { SingleChoice: { key: 'style', options: [{ value: 'tech', label: '科技' }] } }
    });
    expect(multiChoice('platforms', { key: 'platforms', options: [{ value: 'douyin', label: '抖音' }] })).toEqual({
      id: 'platforms',
      component: { MultiChoice: { key: 'platforms', options: [{ value: 'douyin', label: '抖音' }] } }
    });
    expect(fileUpload('asset', { key: 'reference_file', label: '上传图片', accept: 'image/*' })).toEqual({
      id: 'asset',
      component: { FileUpload: { key: 'reference_file', label: '上传图片', accept: 'image/*' } }
    });
    expect(imagePreview('image', { url: 'https://example.test/a.png', title: '参考图' })).toEqual({
      id: 'image',
      component: { ImagePreview: { url: 'https://example.test/a.png', title: '参考图' } }
    });
    expect(videoPreview('video', { url: 'https://example.test/a.mp4', poster: 'https://example.test/p.png' })).toEqual({
      id: 'video',
      component: { VideoPreview: { url: 'https://example.test/a.mp4', poster: 'https://example.test/p.png' } }
    });
    expect(verticalSteps('steps', { steps: [{ title: '分析', status: 'running' }] })).toEqual({
      id: 'steps',
      component: { VerticalSteps: { steps: [{ title: '分析', status: 'running' }] } }
    });
  });

  it('only resolves exact standard component names', () => {
    expect(componentPayload(a2uiComponent('x', A2UI_COMPONENTS.TEXT, { value: 'ok' }), A2UI_COMPONENTS.TEXT)).toEqual({
      value: 'ok'
    });
    expect(componentPayload({ id: 'x', component: { text: { value: 'legacy' } } }, A2UI_COMPONENTS.TEXT)).toBeNull();
    expect(componentPayload({ id: 'x', component: { Radio: { key: 'legacy' } } }, A2UI_COMPONENTS.SINGLE_CHOICE)).toBeNull();
    expect(componentPayload({ id: 'x', component: { checkbox_group: { key: 'legacy' } } }, A2UI_COMPONENTS.MULTI_CHOICE)).toBeNull();
    expect(componentPayload({ id: 'x', component: { rich_text: { value: 'legacy' } } }, A2UI_COMPONENTS.MARKDOWN)).toBeNull();
  });

  it('builds business message cards by extending the base card protocol class', () => {
    const messageCard = createInfoCollectionCard({
      title: '补充产品信息',
      root: 'root',
      submit_label: '提交信息',
      components: [
        card('root', ['product']),
        textInput('product', { key: 'product_name', label: '产品名称/品类', required: true })
      ]
    });

    expect(messageCard).toBeInstanceOf(A2UICard);
    expect(messageCard).toBeInstanceOf(InfoCollectionCard);
    expect({ ...messageCard }).toEqual({
      card_type: 'info_collection',
      title: '补充产品信息',
      root: 'root',
      submit_label: '提交信息',
      components: [
        { id: 'root', component: { Card: { children: ['product'] } } },
        { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称/品类', required: true } } }
      ]
    });
  });
});
