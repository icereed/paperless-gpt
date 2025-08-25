import React from 'react';
import PromptsEditor from './PromptsEditor';

const Settings: React.FC = () => {
  return (
    <main className="p-4">
      <h1 className="mb-4 text-2xl font-semibold">Settings</h1>
      {/* This component can be extended to include other settings in the future */}
      <PromptsEditor />
    </main>
  );
};

export default Settings;
