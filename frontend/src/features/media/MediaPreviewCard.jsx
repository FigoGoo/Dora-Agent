// MediaPreviewCard 只消费已由严格 Parser 验证的本地开发预览 Card。
export function MediaPreviewCard({ card }) {
  if (!card || card.kind !== 'media_preview') return null;
  const title = card.toolKey === 'generate_media' ? '测试 PNG' : '测试 MP4';
  if (card.status === 'failed') {
    return (
      <article className="media-preview-card media-preview-card--failed" data-media-preview-status="failed">
        <h3>{title} 未完成</h3>
        <p role="alert">错误码：{card.errorCode}</p>
        <footer>结果码：{card.resultCode}</footer>
      </article>
    );
  }
  if (card.status === 'accepted') {
    return (
      <article className="media-preview-card" data-media-preview-status="accepted" data-media-preview-asset-id={card.assetRef.id}>
        <h3>{title} 已受理</h3>
        <p>Worker 正在处理；accepted 不代表产物已经完成。</p>
        <footer>Operation：{card.operationID}</footer>
      </article>
    );
  }
  if (card.status !== 'completed') return null;
  return (
    <article className="media-preview-card" data-media-preview-status="completed" data-media-preview-asset-id={card.assetRef.id}>
      <header><h3>{title} 已完成</h3><span>{card.assetRef.mimeType}</span></header>
      {card.assetRef.mediaKind === 'image'
        ? <img src={card.contentURL} alt="本地确定性媒体开发预览" />
        : <video src={card.contentURL} controls preload="metadata">当前浏览器无法播放该 MP4。</video>}
      <dl>
        <div><dt>Asset</dt><dd>{card.assetRef.id}</dd></div>
        <div><dt>大小</dt><dd>{card.assetRef.sizeBytes} bytes</dd></div>
        <div><dt>SHA-256</dt><dd>{card.assetRef.contentDigest}</dd></div>
      </dl>
      <footer><a href={card.contentURL}>下载受保护产物</a></footer>
    </article>
  );
}
