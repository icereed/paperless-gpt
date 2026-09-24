import React from "react";
import { useSearchParams } from "react-router-dom";
import { useExtensionPages } from "./hooks/useExtensionPages";

/**
 * Shows a page contributed by a linked-in extension inside the app layout.
 * The page is looked up by id among the pages the backend lists, so the
 * frame can never be pointed at an arbitrary URL.
 */
const ExtensionPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const pages = useExtensionPages();
  const page = pages.find((p) => p.id === searchParams.get("page"));

  if (!page) {
    return <div className="p-6 text-sm text-muted">Loading…</div>;
  }
  return (
    <iframe
      key={page.id}
      src={`./${page.url}`}
      title={page.title}
      className="block h-full w-full border-0"
    />
  );
};

export default ExtensionPage;
