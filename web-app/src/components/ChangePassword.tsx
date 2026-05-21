import React, { useState } from 'react';
import { useAuth } from '../context/AuthContext';

interface ChangePasswordProps {
  compact?: boolean;
  force?: boolean;
}

const ChangePassword: React.FC<ChangePasswordProps> = ({ compact = false, force = false }) => {
  const { user, refresh } = useAuth();
  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);
  const [loading, setLoading] = useState(false);

  if (!user) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSuccess(false);
    if (newPw !== confirmPw) {
      setError('New passwords do not match');
      return;
    }
    if (newPw.length < 8) {
      setError('New password must be at least 8 characters');
      return;
    }
    if (currentPw === newPw) {
      setError('New password must be different from the current password');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch('./api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ current_password: currentPw, new_password: newPw }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({})) as { error?: string };
        throw new Error(body.error ?? 'Failed to change password');
      }
      setSuccess(true);
      setCurrentPw('');
      setNewPw('');
      setConfirmPw('');
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to change password');
    } finally {
      setLoading(false);
    }
  };

  const form = (
    <>
      {force && (
        <p className="mb-4 rounded-lg bg-amber-50 px-4 py-2.5 text-sm text-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
          You must change your password before continuing.
        </p>
      )}
      <form onSubmit={(e) => { void handleSubmit(e); }} className="max-w-sm space-y-4">
        <div>
          <label
            htmlFor="cp-current"
            className="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            Current password
          </label>
          <input
            id="cp-current"
            type="password"
            autoComplete="current-password"
            value={currentPw}
            onChange={(e) => setCurrentPw(e.target.value)}
            required
            className="w-full rounded-xl border border-gray-300 bg-white px-4 py-2.5 text-sm text-gray-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          />
        </div>

        <div>
          <label
            htmlFor="cp-new"
            className="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            New password
          </label>
          <input
            id="cp-new"
            type="password"
            autoComplete="new-password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            required
            className="w-full rounded-xl border border-gray-300 bg-white px-4 py-2.5 text-sm text-gray-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
            placeholder="At least 8 characters"
          />
        </div>

        <div>
          <label
            htmlFor="cp-confirm"
            className="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300"
          >
            Confirm new password
          </label>
          <input
            id="cp-confirm"
            type="password"
            autoComplete="new-password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            required
            className="w-full rounded-xl border border-gray-300 bg-white px-4 py-2.5 text-sm text-gray-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          />
        </div>

        {error && (
          <p className="rounded-lg bg-red-50 px-4 py-2.5 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-300">
            {error}
          </p>
        )}
        {success && (
          <p className="rounded-lg bg-green-50 px-4 py-2.5 text-sm text-green-700 dark:bg-green-900/30 dark:text-green-300">
            Password changed successfully.
          </p>
        )}

        <button
          type="submit"
          disabled={loading}
          className="rounded-xl bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50 dark:bg-indigo-500 dark:hover:bg-indigo-600"
        >
          {loading ? 'Saving...' : 'Update password'}
        </button>
      </form>
    </>
  );

  if (compact) {
    return form;
  }

  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <p className="text-sm font-semibold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">
        Account
      </p>
      <h2 className="mt-2 text-xl font-bold text-gray-900 dark:text-gray-100">Change password</h2>
      <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
        Signed in as <span className="font-medium text-gray-700 dark:text-gray-300">{user.username}</span>
      </p>
      <div className="mt-6">{form}</div>
    </section>
  );
};

export default ChangePassword;
