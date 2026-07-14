import { AuthSessionProvider } from '../platform/auth/authSession.js';
import { AppRouter } from './router.jsx';

export function App() {
  return (
    <AuthSessionProvider>
      <AppRouter />
    </AuthSessionProvider>
  );
}
