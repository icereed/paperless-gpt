/**
 * The web app's extension API.
 *
 * A UI extension is compiled into the app at build time: set
 * PGPT_UI_EXTENSION to a module whose default export is a UIExtension, and
 * `vite build` bundles it into the one app artifact. Without it, the empty
 * stub in src/extensions/none.ts is used and the app is unchanged.
 *
 * Extensions import from "@paperless-gpt/app" (this file) only. Everything
 * exported here is the supported surface; other app internals may change at
 * any time.
 */
import type React from "react";
import defaultLogo from "./assets/logo.svg";
import Button from "./components/ui/Button";
import ConfirmDialog from "./components/ui/ConfirmDialog";
import Modal from "./components/ui/Modal";
import Toast from "./components/ui/Toast";

export type { ToastData } from "./components/ui/Toast";
export { Button, ConfirmDialog, Modal, Toast, defaultLogo };

type Icon = React.ComponentType<React.SVGProps<SVGSVGElement>>;

/** A page an extension adds to the app. */
export interface ExtensionRoute {
  /**
   * Single path segment, e.g. "reference-data". Nested paths would break the
   * relative asset base that reverse-proxy prefixes rely on.
   */
  path: string;
  element: React.ReactNode;
  /** Adds a sidebar entry for the page. */
  nav?: { title: string; icon: Icon };
}

export interface UIExtension {
  routes?: ExtensionRoute[];
  /** Wraps the whole app, e.g. to provide shared state or apply a theme. */
  Provider?: React.ComponentType<{ children: React.ReactNode }>;
  /** Replaces the logo and name in the expanded sidebar head. */
  SidebarBrand?: React.ComponentType;
  /** Rendered above the theme toggle at the bottom of the sidebar. */
  SidebarFooter?: React.ComponentType<{ collapsed: boolean }>;
  /** Extra sections at the end of the Settings page. */
  settingsSections?: React.ComponentType[];
  /** Hides the donation appeal in Settings; the version line stays. */
  hideCommunitySupport?: boolean;
}

/** Identity helper that type-checks an extension definition. */
export function defineExtension(extension: UIExtension): UIExtension {
  return extension;
}

/**
 * URL of an HTTP extension's endpoint (mounted by the backend at
 * /extensions/<name>/), relative to the app so reverse-proxy prefixes work.
 */
export function extensionUrl(extensionName: string, path: string): string {
  return `./extensions/${extensionName}/${path.replace(/^\/+/, "")}`;
}
