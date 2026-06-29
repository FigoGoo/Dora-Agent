import { useId } from 'react';
import { Calendar } from 'lucide-react';

const DATE_LIKE_TYPES = new Set(['date', 'datetime-local', 'month', 'time']);

export function TextField({ label, error, hint, textarea = false, className, hideLabel = false, required = false, ...props }) {
  const id = useId();
  const Input = textarea ? 'textarea' : 'input';
  const controlClassName = ['admin-control', error ? 'is-error' : '', className].filter(Boolean).join(' ');
  const dateLike = !textarea && DATE_LIKE_TYPES.has(props.type);
  return (
    <label className="admin-field" htmlFor={id}>
      <span className={hideLabel ? 'admin-sr-only' : undefined}>{label}</span>
      {dateLike ? (
        <span className={`admin-date-input ${error ? 'is-error' : ''}`}>
          <input id={id} className="admin-control" aria-label={label} aria-invalid={Boolean(error)} aria-required={required || undefined} {...props} />
          <Calendar aria-hidden="true" size={15} />
        </span>
      ) : (
        <Input id={id} className={controlClassName} aria-label={label} aria-invalid={Boolean(error)} aria-required={required || undefined} {...props} />
      )}
      {error ? <small className="admin-field__error">{error}</small> : null}
      {!error && hint ? <small>{hint}</small> : null}
    </label>
  );
}
