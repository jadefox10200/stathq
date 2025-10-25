import { Routes, Route } from "react-router-dom";
import Header from "./components/Header";
import Home from "./components/Home";
import InputDailyStats from "./components/InputDailyStats";
import InputWeeklyStats from "./components/InputWeeklyStats";
import ViewDailyStats from "./components/ViewDailyStats";
import ViewWeeklyStats from "./components/ViewWeeklyStats";
import EditStatsView from "./components/EditStatsView";
import ManageStats from "./components/ManageStats";
import AlertModal from "./components/AlertModal";

export default function App() {
  return (
    <div className="ui container">
      <Header />
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/inputDailyStats" element={<InputDailyStats />} />
        <Route path="/inputWeeklyStats" element={<InputWeeklyStats />} />
        <Route path="/viewDailyStats" element={<ViewDailyStats />} />
        <Route path="/viewWeeklyStats" element={<ViewWeeklyStats />} />
        <Route path="/editStatsView" element={<EditStatsView />} />
        <Route path="/manageStats" element={<ManageStats />} />
      </Routes>
      <AlertModal />
    </div>
  );
}
