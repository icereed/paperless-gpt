import {
  mdiCogOutline,
  mdiHistory,
  mdiHomeOutline,
  mdiLogout,
} from "@mdi/js";
import { Icon } from "@mdi/react";
import React, { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import logo from "../assets/logo.svg";
import { useAuth } from "../context/AuthContext";
import "./Sidebar.css";

interface SidebarProps {
  onSelectPage: (page: string) => void;
}

const Sidebar: React.FC<SidebarProps> = ({ onSelectPage }) => {
  const [collapsed, setCollapsed] = useState(false);
  const location = useLocation();
  const { user, logout } = useAuth();

  const toggleSidebar = () => {
    setCollapsed((prev) => !prev);
  };

  const handlePageClick = (page: string) => {
    onSelectPage(page);
  };

  const menuItems = [
    { name: "home", path: "./", icon: mdiHomeOutline, title: "Home" },
    { name: "history", path: "./history", icon: mdiHistory, title: "History" },
    { name: "settings", path: "./settings", icon: mdiCogOutline, title: "Settings" },
  ];

  return (
    <aside
      className={`sidebar ${collapsed ? "collapsed" : ""}`}
      aria-label="Primary navigation"
    >
      <div className={`sidebar-header ${collapsed ? "collapsed" : ""}`}>
        {!collapsed && (
          <div className="sidebar-brand">
            <img
              src={logo}
              alt="Paperless GPT logo"
              className="logo w-8 h-8 object-contain flex-shrink-0"
            />
            <div className="sidebar-brand-copy">
              <span className="sidebar-brand-title">Paperless GPT</span>
              <span className="sidebar-brand-subtitle">Document workflows</span>
            </div>
          </div>
        )}
        <button
          className="toggle-btn"
          onClick={toggleSidebar}
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          type="button"
        >
          &#9776;
        </button>
      </div>
      <nav className="sidebar-nav" aria-label="App sections">
        <ul className="menu-items">
        {menuItems.map((item) => (
          <li key={item.name}>
            <Link
              to={item.path}
              onClick={() => handlePageClick(item.name)}
              className={
                location.pathname.split("/").at(-1) === item.path.split("/").at(-1)
                  ? "menu-link active"
                  : "menu-link"
              }
              aria-current={
                location.pathname.split("/").at(-1) === item.path.split("/").at(-1)
                  ? "page"
                  : undefined
              }
              title={collapsed ? item.title : undefined}
            >
              <div className="menu-icon">
                <Icon path={item.icon} size={1} />
              </div>
              {!collapsed && <span className="menu-label">{item.title}</span>}
            </Link>
          </li>
        ))}
        </ul>
      </nav>

      {/* User section at the bottom */}
      {user && (
        <div className={`sidebar-footer ${collapsed ? "collapsed" : ""}`} style={{ marginTop: "auto", padding: collapsed ? "0.5rem" : "0.75rem 1rem", borderTop: "1px solid var(--border-color, #e5e7eb)" }}>
          {!collapsed && (
            <p className="mb-1 truncate text-xs font-medium text-gray-500 dark:text-gray-400" title={user.username}>
              {user.username}
            </p>
          )}
          <button
            onClick={() => { void logout(); }}
            className="menu-link w-full"
            title={collapsed ? "Sign out" : undefined}
            aria-label="Sign out"
            type="button"
          >
            <div className="menu-icon">
              <Icon path={mdiLogout} size={1} />
            </div>
            {!collapsed && <span className="menu-label">Sign out</span>}
          </button>
        </div>
      )}
    </aside>
  );
};

export default Sidebar;
