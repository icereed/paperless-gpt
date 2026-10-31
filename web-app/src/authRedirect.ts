import axios from "axios";

// With PAPERLESS_FORWARD_AUTH set, the backend answers API calls with 401 and a
// login_url once the paperless-ngx session has expired. Send the browser to
// that login instead of leaving every view with a generic error.
let redirecting = false;

function redirectToLogin(loginUrl: unknown): void {
  if (redirecting || typeof loginUrl !== "string" || loginUrl === "") return;
  redirecting = true;
  const next = window.location.pathname + window.location.search;
  window.location.assign(`${loginUrl}?next=${encodeURIComponent(next)}`);
}

export function installAuthRedirect(): void {
  axios.interceptors.response.use(undefined, (error) => {
    if (axios.isAxiosError(error) && error.response?.status === 401) {
      redirectToLogin(error.response.data?.login_url);
    }
    return Promise.reject(error);
  });

  // A few views still use fetch directly.
  const originalFetch = window.fetch.bind(window);
  window.fetch = async (...args: Parameters<typeof fetch>) => {
    const response = await originalFetch(...args);
    if (response.status === 401) {
      response
        .clone()
        .json()
        .then((body) => redirectToLogin(body?.login_url))
        .catch(() => undefined);
    }
    return response;
  };
}
