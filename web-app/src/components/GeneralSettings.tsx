import React, { useState } from 'react';
import { useAuth } from '../context/AuthContext';
import ChangePassword from './ChangePassword';

const GeneralSettings: React.FC = () => {
  const { user } = useAuth();
  const [showPasswordForm, setShowPasswordForm] = useState(false);

  if (!user) return null;

  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <p className="text-sm font-semibold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">
        General
      </p>
      <h2 className="mt-2 text-xl font-bold text-gray-900 dark:text-gray-100">General settings</h2>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        Manage account-level preferences for this Paperless GPT user.
      </p>

      <div className="mt-6 rounded-2xl border border-gray-200 p-4 dark:border-gray-800">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Password</h3>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
              Signed in as <span className="font-medium text-gray-700 dark:text-gray-300">{user.username}</span>.
            </p>
          </div>
          <button
            type="button"
            onClick={() => setShowPasswordForm((visible) => !visible)}
            className="rounded-xl border border-indigo-200 px-4 py-2 text-sm font-semibold text-indigo-700 transition hover:bg-indigo-50 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 dark:border-indigo-800 dark:text-indigo-300 dark:hover:bg-indigo-950/40"
            aria-expanded={showPasswordForm}
          >
            {showPasswordForm ? 'Cancel password change' : 'Change password'}
          </button>
        </div>

        {showPasswordForm && (
          <div className="mt-6 border-t border-gray-200 pt-6 dark:border-gray-800">
            <ChangePassword compact />
          </div>
        )}
      </div>
    </section>
  );
};

export default GeneralSettings;
