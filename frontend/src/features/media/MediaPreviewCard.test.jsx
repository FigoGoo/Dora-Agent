import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { MediaPreviewCard } from './MediaPreviewCard.jsx';

describe('MediaPreviewCard', () => {
  it('renders accepted without claiming completion', () => {
    render(<MediaPreviewCard card={{
      kind: 'media_preview', toolKey: 'generate_media', status: 'accepted', operationID: 'op',
      assetRef: { id: 'asset' }, resultCode: 'MEDIA_PREVIEW_ACCEPTED'
    }} />);
    expect(screen.getByText('测试 PNG 已受理')).toBeInTheDocument();
    expect(screen.getByText(/不代表产物已经完成/)).toBeInTheDocument();
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('renders protected PNG and playable MP4 only after completed', () => {
    const { rerender } = render(<MediaPreviewCard card={completed('image', 'image/png', '/png')} />);
    expect(screen.getByRole('img')).toHaveAttribute('src', '/png');
    rerender(<MediaPreviewCard card={completed('video', 'video/mp4', '/mp4')} />);
    const video = document.querySelector('video');
    expect(video).toHaveAttribute('controls');
    expect(video).toHaveAttribute('src', '/mp4');
    expect(screen.getByRole('link', { name: '下载受保护产物' })).toHaveAttribute('href', '/mp4');
  });

  it('renders only stable error codes on failure', () => {
    render(<MediaPreviewCard card={{
      kind: 'media_preview', toolKey: 'assemble_output', status: 'failed',
      errorCode: 'ARTIFACT_INVALID', resultCode: 'MEDIA_PREVIEW_FAILED'
    }} />);
    expect(screen.getByRole('alert')).toHaveTextContent('ARTIFACT_INVALID');
  });
});

function completed(mediaKind, mimeType, contentURL) {
  return {
    kind: 'media_preview', toolKey: mediaKind === 'image' ? 'generate_media' : 'assemble_output',
    status: 'completed', contentURL, resultCode: 'MEDIA_PREVIEW_COMPLETED',
    assetRef: { id: 'asset', mediaKind, mimeType, sizeBytes: 10, contentDigest: 'a'.repeat(64) }
  };
}
