import React, { useEffect, useState } from "react";
import { Header, Grid, Loader, Message } from "semantic-ui-react";
import ChartLine from "../components/ChartLine";

const API = process.env.REACT_APP_API_URL || "";

export default function Home() {
  const [stats, setStats] = useState([]);
  const [dataMap, setDataMap] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    async function loadStats() {
      try {
        const sRes = await fetch(`${API}/api/public/stats/view/all`, {
          credentials: "include",
        });
        if (!sRes.ok) {
          throw new Error(`Failed to load stats (status ${sRes.status})`);
        }
        const sJson = await sRes.json();
        const divisionalStats = Array.isArray(sJson)
          ? sJson.filter((s) => s.type === "divisional")
          : [];
        setStats(divisionalStats);

        // Load series for each
        const dataPromises = divisionalStats.map(async (stat) => {
          const res = await fetch(
            `${API}/api/public/stats/${stat.id}/series?view=weekly`,
            {
              credentials: "include",
            }
          );
          if (!res.ok) {
            throw new Error(`Failed to load series for ${stat.short_id}`);
          }
          const json = await res.json();
          const series = Array.isArray(json) ? json : [];
          series.sort((a, b) =>
            a.Weekending < b.Weekending
              ? -1
              : a.Weekending > b.Weekending
              ? 1
              : 0
          );
          const chartData = series.map((r) => ({
            week: r.Weekending,
            value: Number(r.Value),
            author_user_id: r.author_user_id ?? null,
          }));
          return { id: stat.id, data: chartData };
        });

        const results = await Promise.all(dataPromises);
        const newDataMap = {};
        results.forEach(({ id, data }) => {
          newDataMap[id] = data;
        });
        setDataMap(newDataMap);
      } catch (err) {
        console.error(err);
        setError("Failed to load data");
      } finally {
        setLoading(false);
      }
    }
    loadStats();
  }, []);

  if (loading) {
    return <Loader active />;
  }

  if (error) {
    return <Message negative content={error} />;
  }

  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center" className="ui header">
        Divisional Stats
      </Header>
      <Grid columns={3} stackable>
        {stats.map((stat) => (
          <Grid.Column key={stat.id}>
            <h3>
              {stat.full_name} : {stat.division_name}
            </h3>
            <ChartLine
              data={dataMap[stat.id] || []}
              height={300}
              reversed={stat.reversed || false}
            />
          </Grid.Column>
        ))}
      </Grid>
    </div>
  );
}
