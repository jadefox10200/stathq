// Updated InputStats.js — small UX change so the weekly "Enter Value" modal
// uses the currently selected week (weeklyWeek) and no longer asks the user
// to pick the week again inside the modal.
//
// Replace your existing frontend/src/pages/InputStats.js with this file or
// merge the small changes shown below (openWeeklyModal and the Weekly modal JSX).
//
// Note: other functions remain the same as in the aiFix version; this file
// is the full component with only the modal/week behavior adjusted.

import React, { useEffect, useMemo, useState } from "react";
import {
  Container,
  Header,
  Segment,
  Dropdown,
  Button,
  Table,
  Input,
  Message,
  Icon,
  Loader,
  Form,
  Modal,
} from "semantic-ui-react";

const API = process.env.REACT_APP_API_URL || "";

// format YYYY-MM-DD from a Date (UTC-safe)
function formatDateISO(dt) {
  const y = dt.getUTCFullYear();
  const m = `${dt.getUTCMonth() + 1}`.padStart(2, "0");
  const d = `${dt.getUTCDate()}`.padStart(2, "0");
  return `${y}-${m}-${d}`;
}

// compute recent Thursdays matching server behaviour (UTC)
function getRecentThursdays(n = 16) {
  const out = [];
  const now = new Date();
  const todayUTC = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  );
  const todayDow = todayUTC.getUTCDay(); // 0=Sun..6=Sat
  const daysUntilThu = (4 - todayDow + 7) % 7;
  const nextThu = new Date(
    Date.UTC(
      todayUTC.getUTCFullYear(),
      todayUTC.getUTCMonth(),
      todayUTC.getUTCDate() + daysUntilThu
    )
  );
  out.push(formatDateISO(nextThu));
  let d = new Date(nextThu);
  for (let i = 1; i < n; i++) {
    d = new Date(
      Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() - 7)
    );
    out.push(formatDateISO(d));
  }
  return out;
}

// determine which cell corresponds to "today" relative to the weekending (weekISO is Thursday)
function getTodayKeyForWeek(weekISO) {
  if (!weekISO) return "Thursday";
  const parts = weekISO.split("-");
  if (parts.length < 3) return "Thursday";
  const y = parseInt(parts[0], 10);
  const m = parseInt(parts[1], 10) - 1;
  const d = parseInt(parts[2], 10);
  const weekThuUTC = new Date(Date.UTC(y, m, d));
  const now = new Date();
  const nowUTCDate = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  );
  const diffDays = Math.round(
    (nowUTCDate - weekThuUTC) / (1000 * 60 * 60 * 24)
  );
  switch (diffDays) {
    case 0:
      return "Thursday";
    case 1:
      return "Friday";
    case 4:
      return "Monday";
    case 5:
      return "Tuesday";
    case 6:
      return "Wednesday";
    default:
      return "Thursday";
  }
}

export default function InputStats() {
  const thursdays = useMemo(() => getRecentThursdays(20), []);
  const [user, setUser] = useState(null);
  const [isAdmin, setIsAdmin] = useState(false);
  const [statsMeta, setStatsMeta] = useState([]);
  const [metaError, setMetaError] = useState(null);

  // view mode: 'daily' | 'weekly'
  const [viewMode, setViewMode] = useState("daily");

  // Daily state
  const [dailyWeek, setDailyWeek] = useState(thursdays[0]);
  const [dailyTable, setDailyTable] = useState([]);
  const [dailyLoading, setDailyLoading] = useState(false);
  const [dailyMessage, setDailyMessage] = useState(null);

  // Today modal (per-row quick entry) - used by daily
  const [todayModalOpen, setTodayModalOpen] = useState(false);
  const [todayModalStat, setTodayModalStat] = useState(null);
  const [todayModalValue, setTodayModalValue] = useState("");
  const [todayModalSubmitting, setTodayModalSubmitting] = useState(false);

  // Weekly state
  const [weeklyWeek, setWeeklyWeek] = useState(thursdays[0]);
  const [weeklyValues, setWeeklyValues] = useState({}); // map statId -> value (string) for selected week
  const [weeklyLoading, setWeeklyLoading] = useState(false);
  const [weeklyMessage, setWeeklyMessage] = useState(null);
  // weekly quick-entry modal
  const [weeklyModalOpen, setWeeklyModalOpen] = useState(false);
  const [weeklyModalStat, setWeeklyModalStat] = useState(null);
  const [weeklyModalValue, setWeeklyModalValue] = useState("");
  const [weeklyModalSubmitting, setWeeklyModalSubmitting] = useState(false);
  const [weeklyModalTargetUser, setWeeklyModalTargetUser] = useState(null);

  // history list (shown when user requests)
  const [historyList, setHistoryList] = useState([]);
  const [historyLoading, setHistoryLoading] = useState(false);

  // load user + assigned stats
  useEffect(() => {
    (async () => {
      try {
        const uRes = await fetch(`${API}/api/user`, { credentials: "include" });
        if (!uRes.ok) throw new Error("Failed to load user info");
        const uJson = await uRes.json();
        setUser(uJson);
        setIsAdmin(uJson.role === "admin");

        if (uJson.role === "admin") {
          const sr = await fetch(`${API}/api/stats/all`, {
            credentials: "include",
          });
          if (!sr.ok) throw new Error("Failed to load stats metadata (admin)");
          const sj = await sr.json();
          setStatsMeta(Array.isArray(sj) ? sj : []);
        } else {
          const ar = await fetch(`${API}/api/stats/assigned`, {
            credentials: "include",
          });
          if (ar.ok) {
            const aj = await ar.json();
            setStatsMeta(Array.isArray(aj) ? aj : []);
          } else {
            setMetaError(
              "Server must provide GET /api/stats/assigned for non-admin users"
            );
          }
        }
      } catch (err) {
        setMetaError(err.message || String(err));
      }
    })();
  }, []);

  // load daily table when week or assigned stats change
  useEffect(() => {
    if (!statsMeta || !statsMeta.length) {
      setDailyTable([]);
      return;
    }
    if (viewMode === "daily") loadDailyTable(dailyWeek, statsMeta);
  }, [dailyWeek, statsMeta, viewMode]);

  // load weekly values for selected week when week or stats change
  useEffect(() => {
    if (!statsMeta || !statsMeta.length) {
      setWeeklyValues({});
      return;
    }
    if (viewMode === "weekly") loadWeeklyValuesForWeek(weeklyWeek, statsMeta);
  }, [weeklyWeek, statsMeta, viewMode]);

  // load daily data per stat (prefers stat_id query param)
  async function loadDailyTable(week, stats) {
    setDailyLoading(true);
    setDailyMessage(null);
    try {
      const rows = [];
      for (const s of stats) {
        const params = new URLSearchParams();
        if (s.id) params.set("stat_id", String(s.id));
        else params.set("stat", s.short_id || "");
        params.set("date", week);
        const url = `${API}/services/getDailyStats?${params.toString()}`;
        const res = await fetch(url, { credentials: "include" });
        if (!res.ok) {
          rows.push({
            statId: s.id,
            short_id: s.short_id,
            username: s.username,
            full_name: s.full_name,
            type: s.type,
            Thursday: "",
            Friday: "",
            Monday: "",
            Tuesday: "",
            Wednesday: "",
            Quota: "",
          });
          continue;
        }
        const json = await res.json();
        rows.push({
          statId: s.id,
          short_id: s.short_id,
          username: s.username,
          full_name: s.full_name,
          type: s.type,
          Thursday: json.Thursday || "",
          Friday: json.Friday || "",
          Monday: json.Monday || "",
          Tuesday: json.Tuesday || "",
          Wednesday: json.Wednesday || "",
          Quota: json.Quota || "",
        });
      }
      setDailyTable(rows);
    } catch (err) {
      setDailyMessage({ type: "error", text: String(err) });
      setDailyTable([]);
    } finally {
      setDailyLoading(false);
    }
  }

  // Save all daily rows (include StatID per row for unambiguous writes)
  async function saveDailyTable() {
    setDailyLoading(true);
    setDailyMessage(null);
    try {
      const payload = dailyTable.map((r) => ({
        StatID: r.statId || null,
        Name: r.short_id,
        Thursday: r.Thursday || "",
        Friday: r.Friday || "",
        Monday: r.Monday || "",
        Tuesday: r.Tuesday || "",
        Wednesday: r.Wednesday || "",
        Quota: r.Quota || "",
      }));
      const res = await fetch(
        `${API}/services/save7R?thisWeek=${encodeURIComponent(dailyWeek)}`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "include",
          body: JSON.stringify(payload),
        }
      );
      const txt = await res.text();
      if (res.ok) {
        setDailyMessage({ type: "success", text: txt || "Saved" });
        await loadDailyTable(dailyWeek, statsMeta);
      } else {
        setDailyMessage({ type: "error", text: txt || "Failed to save" });
      }
    } catch (err) {
      setDailyMessage({ type: "error", text: String(err) });
    } finally {
      setDailyLoading(false);
    }
  }

  // Open quick-entry modal for a stat row (daily)
  function openTodayModal(row) {
    setTodayModalStat(row);
    setTodayModalValue("");
    setTodayModalOpen(true);
  }

  // Submit today's single value; payload includes StatID so backend updates only that stat
  async function submitTodayValue() {
    if (!todayModalStat) return;
    const dayKey = getTodayKeyForWeek(dailyWeek);
    setDailyTable((prev) =>
      prev.map((r) =>
        r.statId === todayModalStat.statId
          ? { ...r, [dayKey]: todayModalValue }
          : r
      )
    );
    setTodayModalSubmitting(true);
    try {
      const payload = [
        {
          StatID: todayModalStat.statId || null,
          Name: todayModalStat.short_id,
          Thursday: dayKey === "Thursday" ? todayModalValue : "",
          Friday: dayKey === "Friday" ? todayModalValue : "",
          Monday: dayKey === "Monday" ? todayModalValue : "",
          Tuesday: dayKey === "Tuesday" ? todayModalValue : "",
          Wednesday: dayKey === "Wednesday" ? todayModalValue : "",
          Quota: todayModalStat.Quota || "",
        },
      ];
      const res = await fetch(
        `${API}/services/save7R?thisWeek=${encodeURIComponent(dailyWeek)}`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "include",
          body: JSON.stringify(payload),
        }
      );
      const txt = await res.text();
      if (!res.ok) {
        setDailyMessage({
          type: "error",
          text: txt || "Failed to save today's value",
        });
      } else {
        setDailyMessage({
          type: "success",
          text: txt || "Saved today's value",
        });
        await loadDailyTable(dailyWeek, statsMeta);
      }
    } catch (err) {
      setDailyMessage({ type: "error", text: String(err) });
    } finally {
      setTodayModalSubmitting(false);
      setTodayModalOpen(false);
    }
  }

  // ---------- WEEKLY helpers ----------
  // load weekly history for a single stat (returns array of {Weekending, Value})
  async function loadWeeklyHistoryForStat(statOrId) {
    setHistoryLoading(true);
    setHistoryList([]);
    setWeeklyMessage(null);
    try {
      let statId = null;
      let assignedUid = null;

      if (!statOrId) {
        throw new Error("stat id required");
      }

      if (typeof statOrId === "number" || typeof statOrId === "string") {
        statId = String(statOrId);
      } else if (typeof statOrId === "object") {
        // prefer id field names used in repo: id or statId
        statId = String(statOrId.id || statOrId.statId || statOrId.ID);
        // possible assigned user id fields
        assignedUid =
          statOrId.user_id ||
          statOrId.owner_id ||
          (Array.isArray(statOrId.user_ids) && statOrId.user_ids.length
            ? statOrId.user_ids[0]
            : null);
      }

      if (!statId) throw new Error("stat id missing");

      const params = new URLSearchParams();
      params.set("stat_id", statId);

      // include user_id when admin and assignedUid present
      if (isAdmin && assignedUid) {
        params.set("user_id", String(assignedUid));
      }

      const res = await fetch(
        `${API}/services/getWeeklyStats?${params.toString()}`,
        { credentials: "include" }
      );
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt || `HTTP ${res.status}`);
      }
      const json = await res.json();
      setHistoryList(Array.isArray(json) ? json : []);
    } catch (err) {
      setHistoryList([]);
      setWeeklyMessage({ type: "error", text: String(err) });
    } finally {
      setHistoryLoading(false);
    }
  }

  // load values for a specific week for all statsMeta and populate weeklyValues map (statId -> value)
  async function loadWeeklyValuesForWeek(weekISO, stats) {
    setWeeklyLoading(true);
    setWeeklyMessage(null);
    try {
      const map = {};
      for (const s of stats) {
        // default empty for missing id
        if (!s.id) {
          map[s.short_id] = "";
          continue;
        }

        const params = new URLSearchParams();
        params.set("stat_id", String(s.id));

        // If current viewer is admin and the stat metadata contains an assigned user id,
        // include user_id so server returns that user's personal series.
        // Accept common possible field names (user_id, owner_id, or user_ids array).
        const assignedUid =
          s.user_id ||
          s.owner_id ||
          (Array.isArray(s.user_ids) && s.user_ids.length
            ? s.user_ids[0]
            : null);

        if (isAdmin && assignedUid) {
          params.set("user_id", String(assignedUid));
        }

        const res = await fetch(
          `${API}/services/getWeeklyStats?${params.toString()}`,
          {
            credentials: "include",
          }
        );
        if (!res.ok) {
          map[s.id] = "";
          continue;
        }

        const series = await res.json(); // array of {Weekending, Value}
        // Find the entry matching the selected week
        const found = (series || []).find(
          (r) =>
            r.Weekending === weekISO ||
            r.week_ending === weekISO ||
            r.WeekEnding === weekISO
        );
        map[s.id] = found ? found.Value : "";
      }
      setWeeklyValues(map);
    } catch (err) {
      setWeeklyMessage({ type: "error", text: String(err) });
      setWeeklyValues({});
    } finally {
      setWeeklyLoading(false);
    }
  }

  // open weekly quick-entry modal for a stat
  function openWeeklyModal(stat) {
    // stat should include .id and .user_id (if assigned) and possibly .division_id
    setWeeklyModalStat(stat);
    setWeeklyModalValue(weeklyValues[stat.id] || "");
    // use the single DB-backed field name user_id
    const assignedUid = stat.user_id || null;
    console.log("uid: ", assignedUid);
    setWeeklyModalTargetUser(assignedUid);

    setWeeklyModalOpen(true);
  }

  // submit single weekly value for the selected week (sends stat_id/date/value to new endpoint)
  async function submitWeeklySingle() {
    if (!weeklyModalStat) return;
    setHistoryLoading(false);
    setWeeklyModalSubmitting(true);
    setWeeklyMessage(null);
    try {
      const statId = weeklyModalStat.id || weeklyModalStat.statId || null;
      if (!statId) throw new Error("stat id missing for selected stat");

      const payload = {
        stat_id: statId,
        date: weeklyWeek, // currently-selected week on the page
        value: weeklyModalValue,
      };

      // For divisional/main stats, include division_id if present in metadata
      if (
        isAdmin &&
        (weeklyModalStat.type === "divisional" ||
          weeklyModalStat.type === "main")
      ) {
        const divisionId = weeklyModalStat.division_id || null;
        if (divisionId) {
          payload.division_id = divisionId;
        }
        // if no division_id provided, backend will try to resolve from stats table
      }

      // If admin is editing a personal stat that belongs to another user, include user_id
      if (
        isAdmin &&
        (weeklyModalStat.type === "personal" || !weeklyModalStat.type) &&
        weeklyModalTargetUser
      ) {
        payload.user_id = weeklyModalTargetUser;
      }

      const res = await fetch(`${API}/services/logWeeklyStats`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      const text = await res.text();
      let json;
      if (text) {
        try {
          json = JSON.parse(text);
        } catch {
          json = { message: text };
        }
      }

      if (!res.ok) {
        const msg =
          json && json.message ? json.message : text || `HTTP ${res.status}`;
        setWeeklyMessage({ type: "error", text: msg });
        setWeeklyModalSubmitting(false);
        // close the modal to match previous behaviour
        setWeeklyModalOpen(false);
        return;
      }

      const successMsg =
        json && json.message ? json.message : "Weekly value saved";
      setWeeklyMessage({ type: "success", text: successMsg });

      setWeeklyModalOpen(false);
      setWeeklyModalValue("");
      setWeeklyModalTargetUser(null);

      // refresh displayed values for the selected week
      await loadWeeklyValuesForWeek(weeklyWeek, statsMeta);
    } catch (err) {
      setWeeklyMessage({ type: "error", text: String(err) });
    } finally {
      setWeeklyModalSubmitting(false);
    }
  }

  // ---------- RENDER helpers ----------
  function renderTopSwitcher() {
    return (
      <Segment clearing>
        <Button.Group>
          <Button
            active={viewMode === "daily"}
            onClick={() => setViewMode("daily")}
          >
            Daily 7R
          </Button>
          <Button
            active={viewMode === "weekly"}
            onClick={() => setViewMode("weekly")}
          >
            Weekly
          </Button>
        </Button.Group>
      </Segment>
    );
  }

  function renderDailyTable() {
    if (dailyLoading) return <Loader active inline="centered" />;
    if (!dailyTable.length)
      return <Message content="No assigned stats to display" />;
    return (
      <>
        <Table celled striped>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>Stat</Table.HeaderCell>
              <Table.HeaderCell>User</Table.HeaderCell>
              <Table.HeaderCell>Type</Table.HeaderCell>
              <Table.HeaderCell>Thursday</Table.HeaderCell>
              <Table.HeaderCell>Friday</Table.HeaderCell>
              <Table.HeaderCell>Monday</Table.HeaderCell>
              <Table.HeaderCell>Tuesday</Table.HeaderCell>
              <Table.HeaderCell>Wednesday</Table.HeaderCell>
              {/* <Table.HeaderCell>Actions</Table.HeaderCell> */}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {dailyTable.map((r) => (
              <Table.Row key={r.statId}>
                <Table.Cell>
                  <strong>{r.short_id}</strong>
                  <div style={{ fontSize: 12, color: "#666" }}>
                    {r.full_name}
                  </div>
                </Table.Cell>
                <Table.Cell>{r.username}</Table.Cell>
                <Table.Cell>{r.type}</Table.Cell>
                <Table.Cell>
                  <Input
                    fluid
                    value={r.Thursday || ""}
                    onChange={(e) =>
                      setDailyTable((prev) =>
                        prev.map((x) =>
                          x === r ? { ...x, Thursday: e.target.value } : x
                        )
                      )
                    }
                  />
                </Table.Cell>
                <Table.Cell>
                  <Input
                    fluid
                    value={r.Friday || ""}
                    onChange={(e) =>
                      setDailyTable((prev) =>
                        prev.map((x) =>
                          x === r ? { ...x, Friday: e.target.value } : x
                        )
                      )
                    }
                  />
                </Table.Cell>
                <Table.Cell>
                  <Input
                    fluid
                    value={r.Monday || ""}
                    onChange={(e) =>
                      setDailyTable((prev) =>
                        prev.map((x) =>
                          x === r ? { ...x, Monday: e.target.value } : x
                        )
                      )
                    }
                  />
                </Table.Cell>
                <Table.Cell>
                  <Input
                    fluid
                    value={r.Tuesday || ""}
                    onChange={(e) =>
                      setDailyTable((prev) =>
                        prev.map((x) =>
                          x === r ? { ...x, Tuesday: e.target.value } : x
                        )
                      )
                    }
                  />
                </Table.Cell>
                <Table.Cell>
                  <Input
                    fluid
                    value={r.Wednesday || ""}
                    onChange={(e) =>
                      setDailyTable((prev) =>
                        prev.map((x) =>
                          x === r ? { ...x, Wednesday: e.target.value } : x
                        )
                      )
                    }
                  />
                </Table.Cell>
                {/* <Table.Cell>
                  <Button size="tiny" onClick={() => openTodayModal(r)}>
                    <Icon name="clock" /> Enter today's
                  </Button>
                </Table.Cell> */}
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
        <div style={{ marginTop: 12 }}>
          <Button primary onClick={saveDailyTable} loading={dailyLoading}>
            Save All
          </Button>
          {dailyMessage && (
            <Message
              positive={dailyMessage.type === "success"}
              negative={dailyMessage.type === "error"}
              content={dailyMessage.text}
            />
          )}
        </div>
      </>
    );
  }

  function renderWeeklyPanel() {
    return (
      <>
        <Form>
          <Form.Group widths="equal">
            <Form.Field>
              <label>Weekending</label>
              <Dropdown
                selection
                options={thursdays.map((d) => ({ key: d, value: d, text: d }))}
                value={weeklyWeek}
                onChange={(_, { value }) => setWeeklyWeek(value)}
              />
            </Form.Field>
          </Form.Group>
        </Form>

        {weeklyLoading ? (
          <Loader active inline="centered" />
        ) : (
          <>
            <Table celled>
              <Table.Header>
                <Table.Row>
                  <Table.HeaderCell>Stat</Table.HeaderCell>
                  {isAdmin && (
                    <>
                      <Table.HeaderCell>User</Table.HeaderCell>
                      <Table.HeaderCell>Division</Table.HeaderCell>
                    </>
                  )}
                  <Table.HeaderCell>Type</Table.HeaderCell>
                  <Table.HeaderCell>Value</Table.HeaderCell>
                  <Table.HeaderCell>Actions</Table.HeaderCell>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {(statsMeta || []).map((s) => (
                  <Table.Row key={s.id}>
                    <Table.Cell>
                      <strong>{s.short_id}</strong>
                      <div style={{ fontSize: 12, color: "#666" }}>
                        {s.full_name}
                      </div>
                    </Table.Cell>
                    {isAdmin && (
                      <>
                        <Table.Cell>{s.username}</Table.Cell>
                        <Table.Cell>{s.division_name}</Table.Cell>
                      </>
                    )}

                    <Table.Cell>{s.type}</Table.Cell>
                    <Table.Cell>{weeklyValues[s.id] ?? ""}</Table.Cell>
                    <Table.Cell>
                      <Button size="tiny" onClick={() => openWeeklyModal(s)}>
                        <Icon name="pencil" /> Enter Value
                      </Button>
                      <Button
                        size="tiny"
                        onClick={() => loadWeeklyHistoryForStat(s)}
                      >
                        <Icon name="list" /> History
                      </Button>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
            {weeklyMessage && (
              <Message
                positive={weeklyMessage.type === "success"}
                negative={weeklyMessage.type === "error"}
                content={weeklyMessage.text}
              />
            )}
            {historyLoading ? (
              <Loader active inline="centered" />
            ) : historyList.length ? (
              <Segment>
                <Header as="h5">History</Header>
                <Table celled>
                  <Table.Header>
                    <Table.Row>
                      <Table.HeaderCell>Weekending</Table.HeaderCell>
                      <Table.HeaderCell>Value</Table.HeaderCell>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {historyList.map((h, i) => (
                      <Table.Row key={i}>
                        <Table.Cell>
                          {h.Weekending || h.week_ending || h.WeekEnding}
                        </Table.Cell>
                        <Table.Cell>{h.Value}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </Segment>
            ) : null}
          </>
        )}
      </>
    );
  }

  return (
    <Container style={{ marginTop: 20 }}>
      <Header as="h2" textAlign="center">
        Input Stats
      </Header>

      {renderTopSwitcher()}

      {viewMode === "daily" ? (
        <Segment>
          <Header as="h4">Daily 7R</Header>
          <Form>
            <Form.Group widths="equal">
              <Form.Field>
                <label>Weekending</label>
                <Dropdown
                  selection
                  options={thursdays.map((d) => ({
                    key: d,
                    value: d,
                    text: d,
                  }))}
                  value={dailyWeek}
                  onChange={(_, { value }) => setDailyWeek(value)}
                />
              </Form.Field>
            </Form.Group>
          </Form>
          {renderDailyTable()}
        </Segment>
      ) : (
        <Segment>
          <Header as="h4">Weekly</Header>
          {renderWeeklyPanel()}
        </Segment>
      )}

      {/* Today's modal */}
      <Modal
        open={todayModalOpen}
        onClose={() => setTodayModalOpen(false)}
        size="small"
      >
        <Modal.Header>
          Enter today's value for{" "}
          {todayModalStat
            ? `${todayModalStat.short_id} — ${todayModalStat.full_name}`
            : ""}
        </Modal.Header>
        <Modal.Content>
          <Form>
            <Form.Field>
              <label>Value</label>
              <Input
                value={todayModalValue}
                onChange={(e) => setTodayModalValue(e.target.value)}
              />
            </Form.Field>
            <p style={{ fontSize: 12, color: "#666" }}>
              This will populate the cell for today based on the selected week.
            </p>
          </Form>
        </Modal.Content>
        <Modal.Actions>
          <Button
            onClick={() => setTodayModalOpen(false)}
            disabled={todayModalSubmitting}
          >
            Cancel
          </Button>
          <Button
            primary
            onClick={submitTodayValue}
            loading={todayModalSubmitting}
          >
            Save
          </Button>
        </Modal.Actions>
      </Modal>

      {/* Weekly modal */}
      <Modal
        open={weeklyModalOpen}
        onClose={() => setWeeklyModalOpen(false)}
        size="small"
      >
        <Modal.Header>
          Enter weekly value for{" "}
          {weeklyModalStat
            ? `${weeklyModalStat.short_id} — ${weeklyModalStat.full_name}`
            : ""}
        </Modal.Header>
        <Modal.Content>
          <Form>
            {/* Removed internal weekending selector — modal now implicitly uses the currently selected week (weeklyWeek) */}
            <Form.Field>
              <label>Value</label>
              <Input
                value={weeklyModalValue}
                onChange={(e) => setWeeklyModalValue(e.target.value)}
              />
            </Form.Field>
            <p style={{ fontSize: 12, color: "#666" }}>
              This will save the value for the currently selected weekending:{" "}
              <strong>{weeklyWeek}</strong>
            </p>
          </Form>
        </Modal.Content>
        <Modal.Actions>
          <Button
            onClick={() => setWeeklyModalOpen(false)}
            disabled={weeklyModalSubmitting}
          >
            Cancel
          </Button>
          <Button
            primary
            onClick={submitWeeklySingle}
            loading={weeklyModalSubmitting}
          >
            Save
          </Button>
        </Modal.Actions>
      </Modal>
    </Container>
  );
}
