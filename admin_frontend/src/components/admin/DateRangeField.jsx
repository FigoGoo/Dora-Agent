import { Calendar } from 'lucide-react';

export function DateRangeField({ label, start, end, onStartChange, onEndChange }) {
  return (
    <fieldset className="admin-date-range">
      <legend>{label}</legend>
      <span className="admin-date-input">
        <input aria-label={`${label}开始`} type="date" value={start || ''} onChange={(event) => onStartChange(event.target.value)} />
        <Calendar aria-hidden="true" size={15} />
      </span>
      <span className="admin-date-range__separator">至</span>
      <span className="admin-date-input">
        <input aria-label={`${label}结束`} type="date" value={end || ''} onChange={(event) => onEndChange(event.target.value)} />
        <Calendar aria-hidden="true" size={15} />
      </span>
    </fieldset>
  );
}
