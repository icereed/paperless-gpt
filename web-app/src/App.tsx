import React, { useEffect, useState } from 'react';
import { Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import { AuthProvider, useAuth } from './context/AuthContext';
import Sidebar from './components/Sidebar';
import DocumentProcessor from './DocumentProcessor';
import ExperimentalOCR from './ExperimentalOCR';
import History from './History';
import Settings from './components/Settings';
import LoginPage from './pages/LoginPage';
import SetupPage from './pages/SetupPage';

interface VersionInfo {
  version: string;
  commit: string;
  buildDate: string;
}

// Inner component so it can access AuthContext
const AppShell: React.FC = () => {
  const { user, loading, setupRequired } = useAuth();
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);

  useEffect(() => {
    const fetchVersion = async () => {
      try {
        const response = await fetch('./api/version');
        if (response.ok) {
          const data = await response.json() as VersionInfo;
          setVersionInfo(data);
        }
      } catch (error) {
        console.error('Failed to fetch version information:', error);
      }
    };
    void fetchVersion();
  }, []);

  // Derive the router basename from the HTML <base> element when set by the
  // server (e.g. when hosted under a sub-path). Fall back to stripping only
  // the known top-level page segments so deep nesting doesn't corrupt it.
  const knownRoutes = ['/', '/experimental-ocr', '/history', '/settings'];
  const deriveBasename = (): string => {
    const base = document.querySelector('base');
    if (base?.href) {
      try {
        return new URL(base.href).pathname.replace(/\/$/, '');
      } catch {
        // ignore malformed base href
      }
    }
    const path = window.location.pathname;
    for (const route of knownRoutes) {
      if (path === route || path.endsWith(route)) {
        return path.slice(0, path.length - route.length);
      }
    }
    return '';
  };
  const basename = deriveBasename();

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-gray-50 dark:bg-gray-950">
        <p className="text-sm text-gray-500 dark:text-gray-400">Loading…</p>
      </div>
    );
  }

  // First-run: no users exist yet
  if (setupRequired) {
    return <SetupPage />;
  }

  // Not logged in
  if (!user) {
    return <LoginPage />;
  }

  return (
    <Router basename={basename}>
      <div className="flex h-screen flex-col">
        <div className="flex flex-1 overflow-hidden">
          <Sidebar onSelectPage={() => undefined} />
          <div className="flex flex-1 flex-col overflow-y-auto bg-gray-50 dark:bg-gray-950">
            <div className="flex-1">
              <Routes>
                <Route path="/" element={<DocumentProcessor />} />
                <Route path="/experimental-ocr" element={<ExperimentalOCR />} />
                <Route path="/history" element={<History />} />
                <Route path="/settings" element={<Settings />} />
              </Routes>
            </div>
            <footer className="border-t border-gray-200 bg-white/90 px-6 py-4 text-center text-sm text-gray-600 backdrop-blur dark:border-gray-800 dark:bg-gray-900/90 dark:text-gray-300">
              {versionInfo && (
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  <span className="font-semibold">paperless-gpt</span> {versionInfo.version}
                  {versionInfo.commit && versionInfo.commit !== 'devCommit' && versionInfo.commit.length >= 7 && (
                    <span className="ml-2">({versionInfo.commit.slice(0, 7)})</span>
                  )}
                </p>
              )}
            </footer>
          </div>
        </div>
      </div>
    </Router>
  );
};

const App: React.FC = () => (
  <AuthProvider>
    <AppShell />
  </AuthProvider>
);

export default App;
