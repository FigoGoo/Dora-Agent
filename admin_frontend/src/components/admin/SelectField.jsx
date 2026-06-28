import { AdminSelect } from './AdminSelect.jsx';

export function SelectField({ label, options = [], error, ...props }) {
  return (
    <div className="admin-field">
      <span>{label}</span>
      <AdminSelect
        ariaLabel={label}
        className={`admin-select--field ${error ? 'is-error' : ''}`}
        value={props.value}
        onChange={(value) => props.onChange?.({ target: { value } })}
        options={options}
        disabled={props.disabled}
      />
      {error ? <small className="admin-field__error">{error}</small> : null}
    </div>
  );
}
