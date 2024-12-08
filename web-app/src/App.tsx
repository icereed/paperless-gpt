// App.tsx or App.jsx
import React from 'react';
import { Route, BrowserRouter as Router, Routes } from 'react-router-dom';
import Sidebar from './components/Sidebar';
import DocumentProcessor from './DocumentProcessor';
import ExperimentalOCR from './ExperimentalOCR'; // New component
import History from './History';

const App: React.FC = () => {
  return (
    <Router>
      <div style={{ display: "flex", height: "100vh" }}>
        <Sidebar onSelectPage={(page) => console.log(page)} />
        <div style={{ flex: 1, overflowY: "auto" }}>
          <Routes>
            <Route path="/" element={<DocumentProcessor />} />
            <Route path="/experimental-ocr" element={<ExperimentalOCR />} />
            <Route path="/history" element={<History />} />
          </Routes>
        </div>
      </div>
    </Router>
  );
};

export default App;
