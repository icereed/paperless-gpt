import React from 'react';
import { Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import Sidebar from './components/Sidebar';
import DocumentProcessor from './DocumentProcessor';
import ExperimentalOCR from './ExperimentalOCR'; // New component
import History from './History';

const App: React.FC = () => {
  return (
    <Router>
      <div className="flex h-screen flex-col">
        <div className="flex flex-1 overflow-hidden">
          <Sidebar onSelectPage={(page) => console.log(page)} />
          <div className="flex flex-1 flex-col overflow-y-auto">
            <div className="flex-1">
              <Routes>
                <Route path="/" element={<DocumentProcessor />} />
                <Route path="/experimental-ocr" element={<ExperimentalOCR />} />
                <Route path="/history" element={<History />} />
              </Routes>
            </div>
            <footer className="border-t-2 border-gray-200 bg-blue-50 p-5 text-center text-base text-gray-700 shadow-[0_-2px_10px_rgba(0,0,0,0.05)] dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:shadow-[0_-2px_10px_rgba(0,0,0,0.2)]">
              <p className="mb-3 font-medium">
                <span role="img" aria-label="coffee" className="text-xl">☕</span>{' '}
                If paperless-gpt just saved you from document chaos, consider fueling future development with a coffee! 
                {' '}<span role="img" aria-label="rocket" className="text-xl">🚀</span>
              </p>
              <a 
                href="https://buymeacoffee.com/icereed" 
                target="_blank" 
                rel="noopener noreferrer" 
                className="inline-block rounded-md bg-yellow-300 px-6 py-2.5 font-semibold text-black no-underline shadow transition hover:bg-yellow-400 hover:shadow-md dark:bg-yellow-400 dark:hover:bg-yellow-500"
                aria-label="Buy me a coffee to support future development"
              >
                Buy Me a Coffee
              </a>
            </footer>
          </div>
        </div>
      </div>
    </Router>
  );
};

export default App;