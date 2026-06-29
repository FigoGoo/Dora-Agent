import { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { AlertCircle, CheckCircle2, Info, TriangleAlert, X } from 'lucide-react';
import { IconButton } from './IconButton.jsx';

const ToastContext = createContext(null);
const icons = {
  danger: AlertCircle,
  warning: TriangleAlert,
  success: CheckCircle2,
  info: Info
};

export function ToastProvider({ children }) {
  const [items, setItems] = useState([]);
  const notify = useCallback((message, tone = 'success', options = {}) => {
    const id = crypto.randomUUID();
    const durationMs = options.durationMs || (tone === 'success' ? 3500 : 6000);
    setItems((current) => [...current, { id, message, tone, ...options }]);
    window.setTimeout(() => setItems((current) => current.filter((item) => item.id !== id)), durationMs);
  }, []);
  const value = useMemo(() => ({ notify }), [notify]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      {items.length ? (
        <div className="admin-toast-region" aria-live="assertive">
          {items.map((item) => {
            const Icon = icons[item.tone] || Info;
            return (
              <div key={item.id} className={`admin-toast admin-toast--${item.tone}`} role="alert">
                <Icon aria-hidden="true" size={22} />
                <div className="admin-toast__content">
                  {item.title ? <strong>{item.title}</strong> : null}
                  <p>{item.message}</p>
                  {item.traceId ? <small>trace_id：{item.traceId}</small> : null}
                </div>
                <IconButton label="关闭提示" icon={X} onClick={() => setItems((current) => current.filter((it) => it.id !== item.id))} />
              </div>
            );
          })}
        </div>
      ) : null}
    </ToastContext.Provider>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
