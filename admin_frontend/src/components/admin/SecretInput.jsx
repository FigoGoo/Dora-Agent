import { Eye, EyeOff } from 'lucide-react';
import { useId, useState } from 'react';
import { IconButton } from './IconButton.jsx';

export function SecretInput({ label, value, onChange, placeholder = '重新填写后保存', error, hint, required = false, id, ...props }) {
  const [visible, setVisible] = useState(false);
  const generatedId = useId();
  const inputId = id || generatedId;
  return (
    <div className="admin-field admin-secret">
      <label htmlFor={inputId}>{label}</label>
      <div>
        <input
          id={inputId}
          type="text"
          value={value}
          onChange={onChange}
          placeholder={placeholder}
          autoComplete="new-password"
          data-lpignore="true"
          data-1p-ignore="true"
          spellCheck="false"
          aria-invalid={Boolean(error)}
          aria-required={required || undefined}
          className={[error ? 'is-error' : '', visible ? '' : 'is-masked'].filter(Boolean).join(' ') || undefined}
          {...props}
        />
        <IconButton
          label={visible ? '隐藏密钥' : '显示输入'}
          icon={visible ? EyeOff : Eye}
          onClick={(event) => {
            event.stopPropagation();
            setVisible((current) => !current);
          }}
        />
      </div>
      {error ? <small className="admin-field__error">{error}</small> : <small>{hint || '已保存密钥不明文回显，只允许重新填写。'}</small>}
    </div>
  );
}
