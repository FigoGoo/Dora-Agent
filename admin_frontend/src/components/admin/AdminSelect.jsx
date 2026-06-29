import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown } from 'lucide-react';

export function AdminSelect({ ariaLabel, value, options = [], onChange, className = '', disabled = false, required = false }) {
  const [open, setOpen] = useState(false);
  const [menuStyle, setMenuStyle] = useState(null);
  const id = useId();
  const rootRef = useRef(null);
  const menuRef = useRef(null);
  const selected = options.find((option) => option.value === value) || options[0];

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    function closeOnOutsideClick(event) {
      if (!rootRef.current?.contains(event.target) && !menuRef.current?.contains(event.target)) {
        setOpen(false);
      }
    }
    document.addEventListener('pointerdown', closeOnOutsideClick);
    return () => document.removeEventListener('pointerdown', closeOnOutsideClick);
  }, [open]);

  useLayoutEffect(() => {
    if (!open) {
      return undefined;
    }
    function updateMenuPosition() {
      const rect = rootRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }
      setMenuStyle({
        top: rect.bottom + 4,
        left: rect.left,
        width: Math.max(rect.width, 168)
      });
    }
    updateMenuPosition();
    window.addEventListener('resize', updateMenuPosition);
    window.addEventListener('scroll', updateMenuPosition, true);
    return () => {
      window.removeEventListener('resize', updateMenuPosition);
      window.removeEventListener('scroll', updateMenuPosition, true);
    };
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

  const menu =
    open && typeof document !== 'undefined'
      ? createPortal(
          <div
            ref={menuRef}
            className="admin-select__menu"
            id={id}
            role="listbox"
            aria-label={ariaLabel}
            style={menuStyle || undefined}
            onPointerDown={(event) => event.stopPropagation()}
          >
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
          </div>,
          document.body
        )
      : null;

  return (
    <span ref={rootRef} className={`admin-select ${className}`} onKeyDown={handleKeyDown}>
      <button
        type="button"
        className="admin-select__trigger"
        aria-label={`${ariaLabel}：${selected?.label || ''}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={id}
        aria-required={required || undefined}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{selected?.label || '-'}</span>
        <ChevronDown aria-hidden="true" size={16} />
      </button>
      {menu}
    </span>
  );
}
