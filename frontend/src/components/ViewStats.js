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
  Radio,
} from "semantic-ui-react";
import ChartLine from "../components/ChartLine";

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

  const [weeksBack, setWeeksBack] = useState(16);

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

  //loadData({ filterType: "user", user: value });
  async function loadData(opts = {}) {
    if (!selectedStatId && !opts.statId) {
      setData([]);
      return;
    }
    const statIdToUse = opts.statId || selectedStatId;
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams();
      params.set("view", "weekly");
      if (
        (opts.filterType || filterType) === "user" &&
        (opts.user || selectedUser)
      ) {
        params.set("user_id", String(opts.user || selectedUser));
      }
      const url = `${API}/api/stats/${statIdToUse}/series?${params.toString()}`;
      const res = await fetch(url, { credentials: "include" });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt || `HTTP ${res.status}`);
      }
      const json = await res.json();
      const series = Array.isArray(json) ? json : [];
      series.sort((a, b) =>
        a.Weekending < b.Weekending ? -1 : a.Weekending > b.Weekending ? 1 : 0
      );
      const trimmed = weeksBack
        ? series.slice(Math.max(0, series.length - weeksBack))
        : series;
      const chartData = trimmed.map((r) => ({
        week: r.Weekending,
        value: Number(r.Value),
        author_user_id: r.author_user_id ?? null,
      }));
      setData(chartData);
    } catch (err) {
      console.error("Failed to load weekly series", err);
      setError("Failed to load weekly series");
    } finally {
      setLoading(false);
    }
  }

  function onSelectStat(value) {
    setSelectedStatId(value);
    const meta = stats.find((s) => String(s.id) === String(value));
    setSelectedStatMeta(meta || null);

    // if (meta) {
    //   if (meta.type === "personal") {
    //     setFilterType("canonical");
    //     console.log(meta);
    //     if (meta.user_id) setSelectedUser(meta.user_id);
    //   } else if (meta.type === "divisional") {
    //     setFilterType("division");
    //     if (meta.division_id) setSelectedDivision(meta.division_id);
    //   } else {
    //     setFilterType("canonical");
    //   }
    // }
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
    const last = data[data.length - 1].value;
    const first = data[0].value;
    const change = last - first;
    const pct = first !== 0 ? (change / Math.abs(first)) * 100 : null;
    return { first, last, change, pct };
  }, [data]);

  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center">
        View Stats
      </Header>

      <Segment>
        <Grid stackable>
          <Grid.Row columns={3}>
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
              <div style={{ marginTop: 8 }}>
                <Button
                  onClick={() => loadData()}
                  size="tiny"
                  basic
                  icon
                  labelPosition="left"
                >
                  <Icon name="chart line" /> Refresh data
                </Button>
                {loadingMeta && <Loader active inline size="tiny" />}
              </div>
            </Grid.Column>

            <Grid.Column>
              <label>Scope / Filter</label>
              <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                <Button
                  toggle
                  active={filterType === "user"}
                  onClick={() => {
                    setFilterType("user");
                    setSelectedDivision(null); // reset division when switching to user
                  }}
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
                >
                  All
                </Button>
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
                      //loadData({ filterType: "user", user: value });
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
                      // No need to loadData here, as it filters the stat options
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
