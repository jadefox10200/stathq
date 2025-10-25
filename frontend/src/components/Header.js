import { NavLink } from "react-router-dom";

export default function Header() {
  return (
    <div className="ui container">
      <img src="/public/siteLogo.png" alt="Site Logo" className="h-12 mb-4" />
      <div className="ui large secondary pointing menu">
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
      </div>
    </div>
  );
}
