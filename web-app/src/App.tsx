import React from 'react';
import { Navigate, Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import Sidebar from './components/Sidebar';
import DocumentProcessor from './DocumentProcessor';
import OCR from './OCR';
import History from './History';
import Settings from './components/Settings';
import AdhocAnalysis from './AdhocAnalysis';
import extension from './extension';

const App: React.FC = () => {
  // Keep the base path (path prefix from reverse-proxy) and remove the app path,
  // convert "/" to "" so Router basename is empty at root.
  const rawBasename = window.location.pathname.replace(/(\/[^/]+)$/, "/");
  const basename = rawBasename === "/" ? "" : rawBasename;
  const app = (
    <Router basename={basename}>
      <div className="flex h-full">
        <Sidebar />
        <main className="flex-1 overflow-y-auto">
          <Routes>
            <Route path="/" element={<DocumentProcessor />} />
            <Route path="/adhoc-analysis" element={<AdhocAnalysis />} />
            {/* Tabs live in a query param (?tab=activity) — a nested path would
                break the relative asset base used for reverse-proxy prefixes. */}
            <Route path="/ocr" element={<OCR />} />
            {/* Historical route: OCR shipped as "experimental" for a long time. */}
            <Route path="/experimental-ocr" element={<Navigate to="/ocr" replace />} />
            <Route path="/history" element={<History />} />
            <Route path="/settings" element={<Settings />} />
            {extension.routes?.map((route) => (
              <Route key={route.path} path={`/${route.path}`} element={route.element} />
            ))}
          </Routes>
        </main>
      </div>
    </Router>
  );
  // A linked-in UI extension may wrap the app (shared state, theme, title).
  const Provider = extension.Provider;
  return Provider ? <Provider>{app}</Provider> : app;
};

export default App;
