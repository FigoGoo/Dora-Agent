import { useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { Modal } from './Modal.jsx';
import { Button } from './Button.jsx';
import { TextField } from './TextField.jsx';

export function ConfirmDialog({
  open,
  title,
  objectLabel,
  impactItems = [],
  tone = 'danger',
  requireReason = true,
  previewToken,
  loading = false,
  onClose,
  onConfirm
}) {
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');

  function submit() {
    if (requireReason && !reason.trim()) {
      setError('请输入操作原因。');
      return;
    }
    onConfirm({ reason: reason.trim(), previewToken });
  }

  return (
    <Modal
      open={open}
      title={title}
      onClose={onClose}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="button" variant={tone} loading={loading} onClick={submit}>
            确认执行
          </Button>
        </>
      }
    >
      <div className={`admin-confirm admin-confirm--${tone}`}>
        <TriangleAlert aria-hidden="true" size={20} />
        <div>
          <strong>{objectLabel}</strong>
          {impactItems.length ? (
            <ul>
              {impactItems.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          ) : null}
        </div>
      </div>
      {requireReason ? (
        <TextField
          label="操作原因"
          textarea
          rows={4}
          value={reason}
          onChange={(event) => {
            setReason(event.target.value);
            setError('');
          }}
          error={error}
        />
      ) : null}
    </Modal>
  );
}
