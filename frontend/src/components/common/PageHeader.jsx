export function PageHeader({ eyebrow, title, copy, children }) {
  return (
    <section className="mock-page__header" aria-labelledby={`${title}-title`}>
      <span className="mock-page__eyebrow">{eyebrow}</span>
      <div>
        <h1 id={`${title}-title`}>{title}</h1>
        <p>{copy}</p>
      </div>
      {children}
    </section>
  );
}
