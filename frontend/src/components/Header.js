import { useState, useEffect } from "react";
import { NavLink, useNavigate } from "react-router-dom";

export default function Header() {
  const navigate = useNavigate();
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    // Fetch user info to check role
    fetch(`${process.env.REACT_APP_API_URL}/api/user`, {
      credentials: "include",
    })
      .then((res) => {
        if (res.ok) {
          return res.json();
        }
        throw new Error("Not authenticated");
      })
      .then((data) => {
        setIsAdmin(data.role === "admin");
      })
      .catch(() => {
        setIsAdmin(false);
      });
  }, []);

  const handleLogout = async () => {
    try {
      const response = await fetch(`${process.env.REACT_APP_API_URL}/logout`, {
        method: "POST",
        credentials: "include",
      });
      if (response.ok) {
        navigate("/login");
      } else {
        console.error("Logout failed");
      }
    } catch (err) {
      console.error("Logout error:", err);
    }
  };

  return (
    <div className="ui container">
      <div className="ui large secondary pointing menu">
        <img src="/public/siteLogo.png" alt="Site Logo" className="h-12 mb-4" />
        <NavLink
          to="/"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          Home
        </NavLink>
        <NavLink
          to="/inputDailyStats"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          Input Daily Stats
        </NavLink>
        <NavLink
          to="/inputWeeklyStats"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          Input Weekly Stats
        </NavLink>
        <NavLink
          to="/viewDailyStats"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          View Daily Stats
        </NavLink>
        <NavLink
          to="/viewWeeklyStats"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          View Weekly Stats
        </NavLink>
        <NavLink
          to="/editStatsView"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          Edit Stats
        </NavLink>
        <NavLink
          to="/manageStats"
          className={({ isActive }) => `item ${isActive ? "active" : ""}`}
        >
          Manage Stats
        </NavLink>
        {isAdmin && (
          <NavLink
            to="/manage-users"
            className={({ isActive }) => `item ${isActive ? "active" : ""}`}
          >
            Manage Users
          </NavLink>
        )}
        <div className="right menu">
          <button className="item" onClick={handleLogout}>
            Logout
          </button>
        </div>
      </div>
    </div>
  );
}
