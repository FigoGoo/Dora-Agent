const requiredNames = [
  'DORA_E2E_USER_EMAIL',
  'DORA_E2E_USER_PASSWORD',
  'DORA_E2E_REVIEWER_EMAIL',
  'DORA_E2E_REVIEWER_PASSWORD',
  'DORA_E2E_GOVERNOR_EMAIL',
  'DORA_E2E_GOVERNOR_PASSWORD'
];

const missing = requiredNames.filter((name) => !String(process.env[name] || '').trim());
if (missing.length > 0) {
  console.error(`W1 real review 缺少必填环境变量：${missing.join(', ')}`);
  process.exit(1);
}
