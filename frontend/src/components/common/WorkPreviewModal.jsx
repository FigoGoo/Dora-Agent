import { X } from 'lucide-react';

export function WorkPreviewModal({ work, onClose, onCreate }) {
  if (!work) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section className="preview-modal" role="dialog" aria-modal="true" aria-labelledby="preview-title" onClick={(event) => event.stopPropagation()}>
        <button className="icon-button preview-modal__close" type="button" aria-label="关闭作品预览" title="关闭" onClick={onClose}>
          <X aria-hidden="true" size={18} />
        </button>
        <img src={work.cover} alt="" />
        <div className="preview-modal__body">
          <span className="work-card__tag transparent-tag">
            {work.type}
          </span>
          <h2 id="preview-title">{work.title}</h2>
          <p>{work.description}</p>
          <div className="preview-modal__meta">
            <span>{work.metric}</span>
            <span>公开喜欢</span>
          </div>
          <button className="start-button" type="button" onClick={() => onCreate(work)}>
            用这个方向创作
          </button>
        </div>
      </section>
    </div>
  );
}
