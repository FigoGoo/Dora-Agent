import { useEffect, useId, useRef, useState } from 'react';
import { Check, ChevronDown } from 'lucide-react';

export function AdminSelect({ ariaLabel, value, options = [], onChange, className = '', disabled = false }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const rootRef = useRef(null);
  const selected = options.find((option) => option.value === value) || options[0];

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    function closeOnOutsideClick(event) {
      if (!rootRef.current?.contains(event.target)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', closeOnOutsideClick);
    return () => document.removeEventListener('mousedown', closeOnOutsideClick);
  }, [open]);

  function choose(option) {
    onChange?.(option.value);
    setOpen(false);
  }

  function handleKeyDown(event) {
    if (event.key === 'Escape') {
      setOpen(false);
    }
  }

  return (
    <span ref={rootRef} className={`admin-select ${className}`} onKeyDown={handleKeyDown}>
      <button
        type="button"
        className="admin-select__trigger"
        aria-label={`${ariaLabel}：${selected?.label || ''}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={id}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{selected?.label || '-'}</span>
        <ChevronDown aria-hidden="true" size={16} />
      </button>
      {open ? (
        <div className="admin-select__menu" id={id} role="listbox" aria-label={ariaLabel}>
          {options.map((option) => {
            const isSelected = option.value === value;
            return (
              <button
                key={String(option.value)}
                type="button"
                className={isSelected ? 'is-selected' : ''}
                role="option"
                aria-selected={isSelected}
                onClick={() => choose(option)}
              >
                {isSelected ? <Check aria-hidden="true" size={15} /> : <span aria-hidden="true" />}
                <span>{option.label}</span>
              </button>
            );
          })}
        </div>
      ) : null}
    </span>
  );
}
