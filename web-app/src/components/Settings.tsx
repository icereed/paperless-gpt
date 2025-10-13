import React from 'react';
import PromptsEditor from './PromptsEditor';
import CustomFieldsEditor from './CustomFieldsEditor';

const Settings: React.FC = () => {
  return (
    <main className="p-4">
      <div className="p-6 bg-gray-100 dark:bg-gray-900">
        <PromptsEditor />
      </div>

      <div className="p-6 bg-gray-100 dark:bg-gray-900">
        <CustomFieldsEditor />
      </div>
    </main>
  );
};

export default Settings;
