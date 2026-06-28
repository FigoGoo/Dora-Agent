import { Eye, EyeOff } from 'lucide-react';
import { useState } from 'react';
import { IconButton } from './IconButton.jsx';

export function SecretInput({ label, value, onChange, placeholder = '重新填写后保存', ...props }) {
  const [visible, setVisible] = useState(false);
  return (
    <label className="admin-field admin-secret">
      <span>{label}</span>
      <div>
        <input type={visible ? 'text' : 'password'} value={value} onChange={onChange} placeholder={placeholder} autoComplete="new-password" {...props} />
        <IconButton label={visible ? '隐藏密钥' : '显示输入'} icon={visible ? EyeOff : Eye} onClick={() => setVisible((current) => !current)} />
      </div>
      <small>已保存密钥不明文回显，只允许重新填写。</small>
    </label>
  );
}
