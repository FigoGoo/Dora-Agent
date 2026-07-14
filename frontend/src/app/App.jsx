import { AuthSessionProvider } from '../platform/auth/authSession.js';
import { SkillCommandLedgerProvider } from '../features/skills/skillCommandLedger.jsx';
import { AppRouter } from './router.jsx';

export function App() {
  return (
    <AuthSessionProvider>
      <SkillCommandLedgerProvider>
        <AppRouter />
      </SkillCommandLedgerProvider>
    </AuthSessionProvider>
  );
}
