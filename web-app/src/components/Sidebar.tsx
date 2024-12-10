import axios from "axios";
import React, { useCallback, useEffect, useState } from 'react';
import "./Sidebar.css";
import { Link, useLocation } from 'react-router-dom';
import { Icon } from '@mdi/react';
import { mdiHomeOutline, mdiTextBoxSearchOutline, mdiHistory } from '@mdi/js';
import logo from "../assets/logo.svg";


interface SidebarProps {
  onSelectPage: (page: string) => void;
}

const Sidebar: React.FC<SidebarProps> = ({ onSelectPage }) => {
  const [collapsed, setCollapsed] = useState(false);
  const location = useLocation();

  const toggleSidebar = () => {
    setCollapsed(!collapsed);
  };

  const handlePageClick = (page: string) => {
    onSelectPage(page);
  };

  // Get whether experimental OCR is enabled
  const [ocrEnabled, setOcrEnabled] = useState(false);
  const fetchOcrEnabled = useCallback(async () => {
    try {
      const res = await axios.get<{ enabled: boolean }>("/api/experimental/ocr");
      setOcrEnabled(res.data.enabled);
    } catch (err) {
      console.error(err);
    }
  }, []);

  useEffect(() => {
    fetchOcrEnabled();
  }, [fetchOcrEnabled]);

  const menuItems = [
    { name: 'home', path: '/', icon: mdiHomeOutline, title: 'Home' },
    { name: 'history', path: '/history', icon: mdiHistory, title: 'History' },
  ];

  // If OCR is enabled, add the OCR menu item
  if (ocrEnabled) {
    menuItems.push({ name: 'ocr', path: '/experimental-ocr', icon: mdiTextBoxSearchOutline, title: 'OCR' });
  }

  return (
    <div className={`sidebar min-w-[64px] ${collapsed ? "collapsed" : ""}`}>
      <div className={`sidebar-header ${collapsed ? "collapsed" : ""}`}>
        {!collapsed && <img src={logo} alt="Logo" className="logo w-8 h-8 object-contain flex-shrink-0" />}
        <button className="toggle-btn" onClick={toggleSidebar}>
          &#9776;
        </button>
      </div>
      <ul className="menu-items">
        {menuItems.map((item) => (
          <li key={item.name} className={location.pathname === item.path ? "active" : ""}>
            <Link
              to={item.path}
              onClick={() => handlePageClick(item.name)}
              style={{ display: 'flex', alignItems: 'center' }}
            >
              {/* <Icon path={item.icon} size={1} />
              {!collapsed && <span>&nbsp; {item.title}</span>} */}
              <div className="w-7 h-7 flex items-center justify-center flex-shrink-0">
                <Icon path={item.icon} size={1} />
              </div>
              {!collapsed && <span className="ml-2">{item.title}</span>}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
};

export default Sidebar;
