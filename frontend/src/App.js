import { Routes, Route, Navigate } from "react-router-dom";
import { useState, useEffect } from "react";
import Header from "./components/Header";
import Home from "./components/Home";
import Login from "./components/Login";
import Register from "./components/Register";
import ManageUsers from "./components/ManageUsers";
import InputDailyStats from "./components/InputDailyStats";
import InputWeeklyStats from "./components/InputWeeklyStats";
import ViewDailyStats from "./components/ViewDailyStats";
import ViewWeeklyStats from "./components/ViewWeeklyStats";
import EditStatsView from "./components/EditStatsView";
import ManageStats from "./components/ManageStats";
import AlertModal from "./components/AlertModal";

function ProtectedRoute({ children, requireAdmin = false }) {
  const [isAuthenticated, setIsAuthenticated] = useState(null);
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    fetch(`${process.env.REACT_APP_API_URL}/api/user`, {
      credentials: "include",
    })
      .then(async (res) => {
        if (res.ok) {
          setIsAuthenticated(true);
          const data = await res.json();
          setIsAdmin(data.role === "admin");
        } else {
          setIsAuthenticated(false);
          setIsAdmin(false);
        }
      })
      .catch(() => {
        setIsAuthenticated(false);
        setIsAdmin(false);
      });
  }, []);

  if (isAuthenticated === null) return <div>Loading...</div>;
  if (!isAuthenticated) return <Navigate to="/login" />;
  if (requireAdmin && !isAdmin) return <Navigate to="/" />;
  return children;
}

export default function App() {
  return (
    <div className="ui container">
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <Header />
              <Home />
            </ProtectedRoute>
          }
        />
        <Route
          path="/manage-users"
          element={
            <ProtectedRoute requireAdmin={true}>
              <Header />
              <ManageUsers />
            </ProtectedRoute>
          }
        />
        <Route
          path="/inputDailyStats"
          element={
            <ProtectedRoute>
              <Header />
              <InputDailyStats />
            </ProtectedRoute>
          }
        />
        <Route
          path="/inputWeeklyStats"
          element={
            <ProtectedRoute>
              <Header />
              <InputWeeklyStats />
            </ProtectedRoute>
          }
        />
        <Route
          path="/viewDailyStats"
          element={
            <ProtectedRoute>
              <Header />
              <ViewDailyStats />
            </ProtectedRoute>
          }
        />
        <Route
          path="/viewWeeklyStats"
          element={
            <ProtectedRoute>
              <Header />
              <ViewWeeklyStats />
            </ProtectedRoute>
          }
        />
        <Route
          path="/editStatsView"
          element={
            <ProtectedRoute>
              <Header />
              <EditStatsView />
            </ProtectedRoute>
          }
        />
        <Route
          path="/manageStats"
          element={
            <ProtectedRoute>
              <Header />
              <ManageStats />
            </ProtectedRoute>
          }
        />
      </Routes>
      <AlertModal />
    </div>
  );
}
