// DeliverablesPanel 渲染无故事板的轻直出交付物（session_deliverable 资产）。
// 资产来源是会话资产单一事实源，由调用方过滤后传入。
export default function DeliverablesPanel({ assets }) {
  if (!assets || assets.length === 0) {
    return null;
  }
  return (
    <section className="deliverables-panel" data-testid="deliverables-panel">
      <h3>交付物</h3>
      <div className="deliverables-grid">
        {assets.map((item) => (
          <figure key={item.id} className="deliverable-item">
            <DeliverableMedia asset={item} />
            <figcaption>{item.filename || item.target_id || item.id}</figcaption>
          </figure>
        ))}
      </div>
    </section>
  );
}

function DeliverableMedia({ asset }) {
  const mime = asset.mime_type || '';
  const kind = asset.kind || '';
  if (kind === 'video' || mime.startsWith('video/')) {
    return <video src={asset.url} controls />;
  }
  if (kind === 'audio' || kind === 'music' || mime.startsWith('audio/')) {
    return <audio src={asset.url} controls />;
  }
  return <img src={asset.url} alt={asset.filename || asset.target_id || asset.id} />;
}
