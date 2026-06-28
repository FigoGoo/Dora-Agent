import { Copy } from 'lucide-react';
import { Button } from './Button.jsx';

export function TraceIdCopy({ traceId }) {
  if (!traceId) {
    return <span>-</span>;
  }
  return (
    <Button type="button" variant="ghost" icon={Copy} onClick={() => navigator.clipboard?.writeText(traceId)}>
      {traceId}
    </Button>
  );
}
