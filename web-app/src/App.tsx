import React from 'react';
import { Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import Sidebar from './components/Sidebar';
import DocumentProcessor from './DocumentProcessor';
import ExperimentalOCR from './ExperimentalOCR';
import History from './History';
import Settings from './components/Settings';
import AdhocAnalysis from './AdhocAnalysis';

const App: React.FC = () => {
  // Keep the base path (path prefix from reverse-proxy) and remove the app path,
  // convert "/" to "" so Router basename is empty at root.
  const rawBasename = window.location.pathname.replace(/(\/[^/]+)$/, "/");
  const basename = rawBasename === "/" ? "" : rawBasename;
  return (
    <Router basename={basename}>
      <div className="flex h-full">
        <Sidebar />
        <main className="flex-1 overflow-y-auto">
          <Routes>
            <Route path="/" element={<DocumentProcessor />} />
            <Route path="/adhoc-analysis" element={<AdhocAnalysis />} />
            <Route path="/experimental-ocr" element={<ExperimentalOCR />} />
            <Route path="/history" element={<History />} />
            <Route path="/settings" element={<Settings />} />
          </Routes>
        </main>
      </div>
    </Router>
  );
};

export default App;
