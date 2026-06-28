import { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { CheckCircle2, X } from 'lucide-react';
import { IconButton } from './IconButton.jsx';

const ToastContext = createContext(null);

export function ToastProvider({ children }) {
  const [items, setItems] = useState([]);
  const notify = useCallback((message, tone = 'success') => {
    const id = crypto.randomUUID();
    setItems((current) => [...current, { id, message, tone }]);
    window.setTimeout(() => setItems((current) => current.filter((item) => item.id !== id)), 3500);
  }, []);
  const value = useMemo(() => ({ notify }), [notify]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="admin-toast-region" aria-live="polite">
        {items.map((item) => (
          <div key={item.id} className={`admin-toast admin-toast--${item.tone}`}>
            <CheckCircle2 aria-hidden="true" size={16} />
            <span>{item.message}</span>
            <IconButton label="关闭提示" icon={X} onClick={() => setItems((current) => current.filter((it) => it.id !== item.id))} />
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
