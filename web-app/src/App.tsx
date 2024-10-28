// App.tsx or App.jsx
import React from 'react';
import { Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import DocumentProcessor from './DocumentProcessor';
import ExperimentalOCR from './ExperimentalOCR'; // New component

const App: React.FC = () => {
  return (
    <Router>
      <Routes>
        <Route path="/" element={<DocumentProcessor />} />
        <Route path="/experimental-ocr" element={<ExperimentalOCR />} />
      </Routes>
    </Router>
  );
};

export default App;
