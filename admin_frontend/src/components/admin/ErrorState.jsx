import { Button } from './Button.jsx';
import { Alert } from './Alert.jsx';

export function ErrorState({ error, onRetry }) {
  return (
    <div className="admin-state">
      <Alert tone="danger" title="加载失败" traceId={error?.traceId}>
        {error?.message || '请求失败，请稍后重试。'}
      </Alert>
      {onRetry ? (
        <Button type="button" variant="secondary" onClick={onRetry}>
          重试
        </Button>
      ) : null}
    </div>
  );
}
