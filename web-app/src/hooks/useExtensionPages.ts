import axios from "axios";
import { useEffect, useState } from "react";

/** A page contributed by a linked-in extension (GET /api/extensions/pages). */
export interface ExtensionPage {
  id: string;
  title: string;
  /** Relative to the app's base path. */
  url: string;
}

/** Loads the extension pages once; an empty list when none are linked in. */
export function useExtensionPages(): ExtensionPage[] {
  const [pages, setPages] = useState<ExtensionPage[]>([]);
  useEffect(() => {
    axios
      .get<ExtensionPage[]>("./api/extensions/pages")
      .then((response) => setPages(response.data ?? []))
      .catch(() => setPages([]));
  }, []);
  return pages;
}
