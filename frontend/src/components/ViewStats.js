import React, { useMemo, useState, useEffect } from "react";
import {
  Header,
  Dropdown,
  Button,
  Segment,
  Grid,
  Loader,
  Message,
  Icon,
} from "semantic-ui-react";
import ChartLine from "../components/ChartLine";
// import StandardChart from "../components/StandardChart";

const API = process.env.REACT_APP_API_URL || "";

export default function ViewStats() {
  const [stats, setStats] = useState([]);
  const [users, setUsers] = useState([]);
  const [divisions, setDivisions] = useState([]);

  const [selectedStatId, setSelectedStatId] = useState(null);
  const [selectedStatMeta, setSelectedStatMeta] = useState(null);

  const [filterType, setFilterType] = useState("all"); // user | division | all
  const [selectedUser, setSelectedUser] = useState(null);
  const [selectedDivision, setSelectedDivision] = useState(null);

  // Series view controls
  const thursdays = useMemo(() => getRecentThursdays(104), []);
  const [seriesView, setSeriesView] = useState("weekly"); // weekly | monthly | yearly
  const [seriesLimit, setSeriesLimit] = useState(12); // 6,12,24,36,52
  const [seriesEndWeek, setSeriesEndWeek] = useState(thursdays[0]); // ISO date YYYY-MM-DD

  // Charts are always line/linear per your instruction
  const [chartType] = useState("line");

  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(false);
  const [loadingMeta, setLoadingMeta] = useState(true);
  const [error, setError] = useState(null);

  // Load dropdown metadata once on mount (every user may view every stat)
  useEffect(() => {
    let mounted = true;
    async function loadMetaOnMount() {
      try {
        const sRes = await fetch(`${API}/api/stats/view/all`, {
          credentials: "include",
        });
        if (!sRes.ok) {
          const txt = await sRes.text();
          throw new Error(
            txt || `Failed to load stats (status ${sRes.status})`
          );
        }
        const sJson = await sRes.json();

        // Try to load users and divisions (may be admin-only) but ignore failures
        let uJson = [];
        try {
          const uRes = await fetch(`${API}/api/users`, {
            credentials: "include",
          });
          if (uRes.ok) uJson = await uRes.json();
        } catch (e) {
          // ignore
        }
        let dJson = [];
        try {
          const dRes = await fetch(`${API}/api/divisions`, {
            credentials: "include",
          });
          if (dRes.ok) dJson = await dRes.json();
        } catch (e) {
          // ignore
        }

        if (!mounted) return;
        setStats(Array.isArray(sJson) ? sJson : []);
        setUsers(Array.isArray(uJson) ? uJson : []);
        setDivisions(Array.isArray(dJson) ? dJson : []);
      } catch (err) {
        console.error("Failed to load metadata", err);
        if (mounted) setError("Failed to load stats metadata");
      } finally {
        if (mounted) setLoadingMeta(false);
      }
    }
    loadMetaOnMount();
    return () => {
      mounted = false;
    };
  }, []);

  // loadData: now requests aggregated series from /services/getStatsData
  async function loadData(opts = {}) {
    const statIdToUse = opts.statId || selectedStatId;
    if (!statIdToUse) {
      setData([]);
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams();
      params.set("stat_id", String(statIdToUse));
      params.set("view", opts.view || seriesView || "weekly");
      params.set("limit", String(opts.limit || seriesLimit || 12));
      const end = opts.end || seriesEndWeek || thursdays[0];
      if (end) params.set("end", end);

      // include user filter if requested
      if ((opts.filterType || filterType) === "user") {
        const uid = opts.user ?? selectedUser;
        if (uid) params.set("user_id", String(uid));
      }

      const url = `${API}/services/getStatsData?${params.toString()}`;
      const res = await fetch(url, { credentials: "include" });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt || `HTTP ${res.status}`);
      }
      const json = await res.json();
      const series = Array.isArray(json) ? json : [];

      // Process based on view
      if ((opts.view || seriesView) === "weekly") {
        // Server may return fewer than `limit` points (only weeks present).
        // Fill missing weeks up to the requested end date to ensure a stable chart.
        const filled = fillWeeklySeries(
          series,
          end,
          Number(opts.limit || seriesLimit || 12)
        );
        setData(filled.map((r) => ({ week: r.week, value: Number(r.value) })));
      } else {
        // monthly/yearly: server returns period strings (YYYY-MM or YYYY)
        // Map to { week: period, value }
        const mapped = (series || []).map((r) => ({
          week: r.Weekending,
          value: Number(r.Value),
        }));
        // Ensure ascending by period
        mapped.sort((a, b) => (a.week < b.week ? -1 : a.week > b.week ? 1 : 0));
        setData(mapped);
      }
    } catch (err) {
      console.error("Failed to load weekly series", err);
      setError("Failed to load series");
      setData([]);
    } finally {
      setLoading(false);
    }
  }

  function onSelectStat(value) {
    setSelectedStatId(value);
    const meta = stats.find((s) => String(s.id) === String(value));
    setSelectedStatMeta(meta || null);

    loadData({ statId: value });
  }

  const statOptions = useMemo(() => {
    let filteredStats = stats;
    if (filterType === "user") {
      if (selectedUser && selectedUser !== "all") {
        filteredStats = stats.filter((s) => s.user_id === selectedUser);
      } else {
        filteredStats = stats.filter((s) => s.type === "personal");
      }
    } else if (filterType === "division") {
      filteredStats = stats.filter((s) => s.type === "divisional");
      if (selectedDivision && selectedDivision !== "all") {
        filteredStats = filteredStats.filter(
          (s) => s.division_id === selectedDivision
        );
      }
    }
    // For "all", no filter
    return filteredStats.map((s) => ({
      key: s.id,
      text: `${s.short_id} — ${s.full_name} (${s.type || "personal"})`,
      value: s.id,
    }));
  }, [stats, filterType, selectedDivision, selectedUser]);

  const userOptions = useMemo(
    () => users.map((u) => ({ key: u.id, text: u.username, value: u.id })),
    [users]
  );
  const divisionOptions = useMemo(
    () => divisions.map((d) => ({ key: d.id, text: d.name, value: d.id })),
    [divisions]
  );

  const numericSummary = useMemo(() => {
    if (!data || data.length === 0) return null;
    const first = data[0].value;
    const last = data[data.length - 1].value;
    const change = last - first;
    const pct = first !== 0 ? (change / Math.abs(first)) * 100 : null;
    return { first, last, change, pct };
  }, [data]);

  const viewOptions = [
    { key: "weekly", text: "Weekly", value: "weekly" },
    { key: "monthly", text: "Monthly", value: "monthly" },
    { key: "yearly", text: "Yearly", value: "yearly" },
  ];
  const limitOptions = [6, 12, 24, 36, 52].map((n) => ({
    key: n,
    text: String(n),
    value: n,
  }));

  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center">
        View Stats
      </Header>

      <Segment>
        <Grid stackable>
          {/* First row: Choose Stat + Scope/Filter (Refresh moved to the right side of Scope/Filter) */}
          <Grid.Row columns={2}>
            <Grid.Column>
              <label>Choose Stat</label>
              <Dropdown
                placeholder="Select stat"
                fluid
                search
                selection
                options={statOptions}
                value={selectedStatId}
                onChange={(_, { value }) => onSelectStat(value)}
                noResultsMessage="No stats loaded"
              />
            </Grid.Column>

            <Grid.Column>
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "flex-start",
                }}
              >
                <div>
                  <label>Scope / Filter</label>
                  <div
                    style={{
                      display: "flex",
                      gap: 8,
                      alignItems: "center",
                      marginTop: 4,
                    }}
                  >
                    <Button
                      toggle
                      active={filterType === "user"}
                      onClick={() => {
                        setFilterType("user");
                        setSelectedDivision(null); // reset division when switching to user
                      }}
                      size="tiny"
                    >
                      Personal
                    </Button>
                    <Button
                      toggle
                      active={filterType === "division"}
                      onClick={() => {
                        setFilterType("division");
                        setSelectedUser(null); // reset user when switching to division
                      }}
                      size="tiny"
                    >
                      Division
                    </Button>
                    <Button
                      toggle
                      active={filterType === "all"}
                      onClick={() => {
                        setFilterType("all");
                        setSelectedUser(null);
                        setSelectedDivision(null);
                      }}
                      size="tiny"
                    >
                      All
                    </Button>
                  </div>
                </div>

                {/* Refresh button aligned to the right of the Scope/Filter column */}
                <div style={{ marginLeft: 12 }}>
                  <Button
                    onClick={() =>
                      loadData({
                        statId: selectedStatId,
                        view: seriesView,
                        limit: seriesLimit,
                        end: seriesEndWeek,
                      })
                    }
                    size="tiny"
                    basic
                    icon
                    labelPosition="left"
                  >
                    <Icon name="chart line" /> Refresh
                  </Button>
                </div>
              </div>

              {filterType === "user" && (
                <div style={{ display: "flex", marginTop: 8, gap: 8 }}>
                  <Dropdown
                    placeholder="Select user"
                    search
                    selection
                    options={userOptions}
                    value={selectedUser}
                    onChange={(_, { value }) => {
                      setSelectedUser(value);
                    }}
                  />
                  <Button
                    onClick={() => {
                      setSelectedUser("");
                    }}
                  >
                    Clear
                  </Button>
                </div>
              )}

              {filterType === "division" && (
                <div
                  style={{
                    display: "flex",
                    marginTop: 8,
                    gap: 8,
                  }}
                >
                  <Dropdown
                    placeholder="Select division"
                    search
                    selection
                    options={divisionOptions}
                    value={selectedDivision}
                    onChange={(_, { value }) => {
                      setSelectedDivision(value);
                    }}
                  />
                  <Button
                    onClick={() => {
                      setSelectedDivision("");
                    }}
                  >
                    Clear
                  </Button>
                </div>
              )}
            </Grid.Column>
          </Grid.Row>

          {/* Second row: Series Controls spanning full width */}
          <Grid.Row columns={1}>
            <Grid.Column>
              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  alignItems: "center",
                  gap: 12,
                }}
              >
                <div
                  style={{
                    display: "flex",
                    gap: 8,
                    alignItems: "center",
                    flexWrap: "wrap",
                  }}
                >
                  <Dropdown
                    selection
                    options={viewOptions}
                    value={seriesView}
                    onChange={(_, { value }) => setSeriesView(value)}
                  />
                  <Dropdown
                    selection
                    options={limitOptions}
                    value={seriesLimit}
                    onChange={(_, { value }) => setSeriesLimit(value)}
                  />
                  <Dropdown
                    selection
                    options={thursdays.map((d) => ({
                      key: d,
                      text: d,
                      value: d,
                    }))}
                    value={seriesEndWeek}
                    onChange={(_, { value }) => setSeriesEndWeek(value)}
                  />
                  <Button
                    primary
                    onClick={() =>
                      loadData({
                        statId: selectedStatId,
                        view: seriesView,
                        limit: seriesLimit,
                        end: seriesEndWeek,
                      })
                    }
                  >
                    Apply
                  </Button>
                </div>
              </div>

              <div style={{ marginTop: 8, fontSize: 12, color: "#666" }}>
                View: weekly / monthly / yearly. Points: number of data points
                to display. End Week: the last week (Thursday) included in the
                range.
              </div>
            </Grid.Column>
          </Grid.Row>
        </Grid>
      </Segment>

      {error && <Message negative content={error} />}

      <Segment style={{ minHeight: 300 }}>
        {loading ? (
          <Loader active inline="centered" />
        ) : data.length === 0 ? (
          <Message
            info
            content="No data to display. Choose a stat and click Refresh."
          />
        ) : (
          <ChartLine
            data={data}
            height={360}
            reversed={selectedStatMeta?.reversed || false}
          />
        )}
      </Segment>

      {numericSummary && (
        <Segment>
          <strong>Latest:</strong> {numericSummary.last} &nbsp;
          <strong>Change:</strong> {numericSummary.change}{" "}
          {numericSummary.pct !== null && (
            <>( {numericSummary.pct.toFixed(1)}% )</>
          )}
        </Segment>
      )}
    </div>
  );
}

/* Helpers */

// Generate recent Thursdays (most recent first). n = number of Thursdays to generate.
function getRecentThursdays(n = 104) {
  const out = [];
  const now = new Date();
  // Use UTC based day to match server validation which expects Thursday.
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
  out.push(nextThu.toISOString().slice(0, 10));
  let d = new Date(nextThu);
  for (let i = 1; i < n; i++) {
    d = new Date(
      Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() - 7)
    );
    out.push(d.toISOString().slice(0, 10));
  }
  return out;
}

// Fill weekly series to ensure exactly `limit` points ending at endWeekISO (inclusive).
// `results`: array of { Weekending: 'YYYY-MM-DD', Value: number } ascending or unordered.
function fillWeeklySeries(results, endWeekISO, limit) {
  const map = new Map();
  (results || []).forEach((r) => {
    const key = r.Weekending || r.week;
    map.set(key, r.Value ?? r.value ?? 0);
  });

  const out = [];
  const end = new Date(endWeekISO + "T00:00:00Z");
  let d = new Date(end);
  for (let i = 0; i < limit; i++) {
    const iso = d.toISOString().slice(0, 10);
    const val = map.has(iso) ? map.get(iso) : 0;
    // unshift to build ascending array
    out.unshift({ week: iso, value: val });
    d = new Date(d.getTime() - 7 * 24 * 3600 * 1000);
  }
  return out;
}
