import {
  ClockIcon,
  Cog6ToothIcon,
  DocumentChartBarIcon,
  DocumentMagnifyingGlassIcon,
  HomeIcon,
  Bars3Icon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import logo from "../assets/logo.svg";
import ThemeToggle from "./ThemeToggle";

const COLLAPSE_KEY = "pgpt-sidebar-collapsed";

interface MenuItem {
  name: string;
  path: string;
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  title: string;
}

const Sidebar: React.FC = () => {
  const [collapsed, setCollapsed] = useState(
    () =>
      localStorage.getItem(COLLAPSE_KEY) === "1" ||
      window.matchMedia("(max-width: 767px)").matches
  );
  const location = useLocation();

  // Small screens force the rail; the toggle can still expand it on demand.
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 767px)");
    const onChange = () => {
      if (mq.matches) setCollapsed(true);
    };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  const toggleSidebar = () => {
    setCollapsed((prev) => {
      localStorage.setItem(COLLAPSE_KEY, prev ? "0" : "1");
      return !prev;
    });
  };

  // OCR is the headline feature and always visible — without a configured
  // provider the page shows setup guidance instead of hiding entirely.
  const menuItems: MenuItem[] = [
    { name: "home", path: "./", icon: HomeIcon, title: "Home" },
    {
      name: "ocr",
      path: "./ocr",
      icon: DocumentMagnifyingGlassIcon,
      title: "OCR",
    },
    {
      name: "adhoc-analysis",
      path: "./adhoc-analysis",
      icon: DocumentChartBarIcon,
      title: "Ad-hoc Analysis",
    },
    { name: "history", path: "./history", icon: ClockIcon, title: "History" },
    {
      name: "settings",
      path: "./settings",
      icon: Cog6ToothIcon,
      title: "Settings",
    },
  ];

  const currentSegment = location.pathname.split("/").at(-1);

  return (
    <div
      className={classNames(
        "flex shrink-0 flex-col border-r border-line bg-surface-2",
        collapsed ? "w-16" : "w-60"
      )}
    >
      <div
        className={classNames(
          "flex h-14 items-center border-b border-line px-3",
          collapsed ? "justify-center" : "justify-between"
        )}
      >
        {!collapsed && (
          <span className="flex min-w-0 items-center gap-2">
            <img src={logo} alt="" className="h-7 w-7 shrink-0 object-contain" />
            <span className="truncate text-sm font-semibold">paperless-gpt</span>
          </span>
        )}
        <button
          type="button"
          onClick={toggleSidebar}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="rounded-md p-2 text-muted transition-colors duration-150 ease-out-quart hover:bg-surface hover:text-ink"
        >
          <Bars3Icon className="h-5 w-5" aria-hidden="true" />
        </button>
      </div>

      <nav aria-label="Main" className="flex-1 overflow-y-auto p-2">
        <ul className="space-y-1">
          {menuItems.map((item) => {
            // /ocr has sub-routes (/ocr/activity) that keep the item active.
            const isActive =
              item.name === "ocr"
                ? location.pathname.includes("/ocr")
                : currentSegment === item.path.split("/").at(-1);
            const Icon = item.icon;
            return (
              <li key={item.name}>
                <Link
                  to={item.path}
                  aria-current={isActive ? "page" : undefined}
                  aria-label={collapsed ? item.title : undefined}
                  title={collapsed ? item.title : undefined}
                  className={classNames(
                    "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors duration-150 ease-out-quart",
                    collapsed && "justify-center px-2",
                    isActive
                      ? "bg-primary-tint font-medium text-ink"
                      : "text-muted hover:bg-surface hover:text-ink"
                  )}
                >
                  <Icon className="h-5 w-5 shrink-0" aria-hidden="true" />
                  {!collapsed && <span className="truncate">{item.title}</span>}
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>

      <div className="border-t border-line p-2">
        <ThemeToggle showLabel={!collapsed} />
      </div>
    </div>
  );
};

export default Sidebar;
