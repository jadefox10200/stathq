// InputDailyStats.js   (plain JavaScript – no TypeScript)
import { useState, useEffect } from "react";
import {
  Form,
  Button,
  Dropdown,
  Input,
  Table,
  Message,
  Header,
  Icon,
  Segment,
} from "semantic-ui-react";

const API = process.env.REACT_APP_API_URL || "";

export default function InputDailyStats() {
  /* ---------- Daily Form ---------- */
  const [stats, setStats] = useState([]); // [{key, text, value}]
  const [divisions, setDivisions] = useState([]); // [{key, text, value}]
  const [selStat, setSelStat] = useState("");
  const [selDiv, setSelDiv] = useState("");
  const [dailyDate, setDailyDate] = useState("");
  const [dailyValue, setDailyValue] = useState("");

  /* ---------- Weekly Form ---------- */
  const [weeklyDate, setWeeklyDate] = useState("");
  const [vsd, setVsd] = useState("");
  const [gi, setGi] = useState("");
  const [sites, setSites] = useState("");
  const [expenses, setExpenses] = useState("");
  const [scheduled, setScheduled] = useState("");
  const [outstanding, setOutstanding] = useState("");

  /* ---------- Edit Table ---------- */
  const [weeklyRows, setWeeklyRows] = useState([]); // array of objects
  const [editingRow, setEditingRow] = useState(null);

  /* ---------- UI Feedback ---------- */
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  /* ----------------------------------------------------------- */
  /*  Load reference data (stats + divisions)                    */
  /* ----------------------------------------------------------- */
  useEffect(() => {
    Promise.all([
      fetch(`${API}/api/stats`, { credentials: "include" }).then((r) =>
        r.json()
      ),
      fetch(`${API}/api/divisions`, { credentials: "include" }).then((r) =>
        r.json()
      ),
    ])
      .then(([statData, divData]) => {
        setStats(
          statData.map((s) => ({
            key: s.id,
            text: s.name,
            value: s.id,
          }))
        );
        setDivisions(
          divData.map((d) => ({
            key: d.id,
            text: d.name,
            value: d.id,
          }))
        );
      })
      .catch(() => setError("Failed to load reference data"));
  }, []);

  /* ----------------------------------------------------------- */
  /*  Load weekly table for editing                               */
  /* ----------------------------------------------------------- */
  const loadWeeklyTable = () => {
    fetch(`${API}/services/getWeeklyStats`, { credentials: "include" })
      .then((r) => r.json())
      .then((data) => setWeeklyRows(data))
      .catch(() => setError("Failed to load weekly stats"));
  };
  useEffect(() => loadWeeklyTable(), []);

  /* ----------------------------------------------------------- */
  /*  Helper – flash a message for 3 s                           */
  /* ----------------------------------------------------------- */
  const flash = (msg, isErr = false) => {
    if (isErr) setError(msg);
    else setSuccess(msg);
    setTimeout(() => {
      setError("");
      setSuccess("");
    }, 3000);
  };

  /* ----------------------------------------------------------- */
  /*  DAILY SUBMIT                                               */
  /* ----------------------------------------------------------- */
  const submitDaily = async (e) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    if (!selStat || !selDiv || !dailyDate || !dailyValue) {
      flash("All daily fields are required", true);
      return;
    }

    try {
      const resp = await fetch(`${API}/services/save7R`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({
          stat_id: selStat,
          division_id: selDiv,
          date: dailyDate,
          value: Number(dailyValue),
        }),
      });

      const txt = await resp.text();
      if (resp.ok) {
        flash("Daily stat saved");
        setSelStat("");
        setSelDiv("");
        setDailyDate("");
        setDailyValue("");
      } else {
        flash(txt || "Failed to save daily stat", true);
      }
    } catch {
      flash("Network error", true);
    }
  };

  /* ----------------------------------------------------------- */
  /*  WEEKLY SUBMIT                                              */
  /* ----------------------------------------------------------- */
  const submitWeekly = async (e) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    if (!weeklyDate || !vsd || !gi || !sites || !expenses || !scheduled) {
      flash("All weekly fields are required", true);
      return;
    }

    try {
      const resp = await fetch(`${API}/services/logWeeklyStats`, {
        method: "POST",
        credentials: "include",
        body: new URLSearchParams({
          date: weeklyDate,
          vsd,
          gi,
          sites,
          expenses,
          scheduled,
          outstanding: outstanding || "0",
        }),
      });

      const txt = await resp.text();
      if (resp.ok) {
        flash("Weekly stat saved");
        setWeeklyDate("");
        setVsd("");
        setGi("");
        setSites("");
        setExpenses("");
        setScheduled("");
        setOutstanding("");
      } else {
        flash(txt || "Failed to save weekly stat", true);
      }
    } catch {
      flash("Network error", true);
    }
  };

  /* ----------------------------------------------------------- */
  /*  EDIT TABLE – SAVE                                          */
  /* ----------------------------------------------------------- */
  const saveWeeklyEdits = async () => {
    try {
      const resp = await fetch(`${API}/services/saveWeeklyEdit`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(weeklyRows),
      });

      const txt = await resp.text();
      if (resp.ok) {
        flash("Weekly edits saved");
        loadWeeklyTable();
      } else {
        flash(txt || "Failed to save edits", true);
      }
    } catch {
      flash("Network error", true);
    }
  };

  /* ----------------------------------------------------------- */
  /*  EDIT TABLE – CELL UPDATE                                   */
  /* ----------------------------------------------------------- */
  const updateCell = (idx, field, value) => {
    const rows = [...weeklyRows];
    rows[idx][field] =
      typeof rows[idx][field] === "number" ? Number(value) : value;
    setWeeklyRows(rows);
  };

  /* ----------------------------------------------------------- */
  /*  RENDER                                                     */
  /* ----------------------------------------------------------- */
  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center">
        Stat HQ – Input & Edit Stats
      </Header>

      {/* GLOBAL MESSAGES */}
      {error && <Message negative>{error}</Message>}
      {success && <Message positive>{success}</Message>}

      {/* ---------- DAILY INPUT ---------- */}
      <Segment raised>
        <Header as="h3" dividing>
          <Icon name="calendar plus outline" />
          Input Daily Stat
        </Header>
        <Form onSubmit={submitDaily}>
          <Form.Group widths="equal">
            <Form.Field required>
              <label>Stat</label>
              <Dropdown
                placeholder="Select stat"
                selection
                options={stats}
                value={selStat}
                onChange={(_, { value }) => setSelStat(value)}
              />
            </Form.Field>
            <Form.Field required>
              <label>Division</label>
              <Dropdown
                placeholder="Select division"
                selection
                options={divisions}
                value={selDiv}
                onChange={(_, { value }) => setSelDiv(value)}
              />
            </Form.Field>
          </Form.Group>

          <Form.Group widths="equal">
            <Form.Field required>
              <label>Date</label>
              <Input
                type="date"
                value={dailyDate}
                onChange={(e) => setDailyDate(e.target.value)}
              />
            </Form.Field>
            <Form.Field required>
              <label>Value</label>
              <Input
                type="number"
                value={dailyValue}
                onChange={(e) => setDailyValue(e.target.value)}
              />
            </Form.Field>
          </Form.Group>

          <Button primary type="submit">
            Save Daily Stat
          </Button>
        </Form>
      </Segment>

      {/* ---------- WEEKLY INPUT ---------- */}
      <Segment raised style={{ marginTop: "2rem" }}>
        <Header as="h3" dividing>
          <Icon name="calendar alternate outline" />
          Input Weekly Stat
        </Header>
        <Form onSubmit={submitWeekly}>
          <Form.Field required>
            <label>Week Ending (YYYY-MM-DD)</label>
            <Input
              type="date"
              value={weeklyDate}
              onChange={(e) => setWeeklyDate(e.target.value)}
            />
          </Form.Field>

          <Form.Group widths={3}>
            <Form.Field required>
              <label>VSD</label>
              <Input value={vsd} onChange={(e) => setVsd(e.target.value)} />
            </Form.Field>
            <Form.Field required>
              <label>GI</label>
              <Input value={gi} onChange={(e) => setGi(e.target.value)} />
            </Form.Field>
            <Form.Field required>
              <label>Sites</label>
              <Input value={sites} onChange={(e) => setSites(e.target.value)} />
            </Form.Field>
          </Form.Group>

          <Form.Group widths={3}>
            <Form.Field required>
              <label>Expenses</label>
              <Input
                value={expenses}
                onChange={(e) => setExpenses(e.target.value)}
              />
            </Form.Field>
            <Form.Field required>
              <label>Scheduled</label>
              <Input
                value={scheduled}
                onChange={(e) => setScheduled(e.target.value)}
              />
            </Form.Field>
            <Form.Field>
              <label>Outstanding (optional)</label>
              <Input
                value={outstanding}
                onChange={(e) => setOutstanding(e.target.value)}
              />
            </Form.Field>
          </Form.Group>

          <Button primary type="submit">
            Save Weekly Stat
          </Button>
        </Form>
      </Segment>

      {/* ---------- EDIT / CORRECT PAST WEEKS ---------- */}
      <Segment raised style={{ marginTop: "2rem" }}>
        <Header as="h3" dividing>
          <Icon name="edit" />
          Edit / Correct Past Weeks
        </Header>

        <Table celled structured>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>Week Ending</Table.HeaderCell>
              <Table.HeaderCell>VSD</Table.HeaderCell>
              <Table.HeaderCell>GI</Table.HeaderCell>
              <Table.HeaderCell>Expenses</Table.HeaderCell>
              <Table.HeaderCell>Sites</Table.HeaderCell>
              <Table.HeaderCell>Scheduled</Table.HeaderCell>
              <Table.HeaderCell>Outstanding</Table.HeaderCell>
            </Table.Row>
          </Table.Header>

          <Table.Body>
            {weeklyRows.map((row, i) => (
              <Table.Row key={i}>
                <Table.Cell>{row.WeekEnding}</Table.Cell>

                {[
                  "VSD",
                  "GI",
                  "Expenses",
                  "Sites",
                  "Scheduled",
                  "Outstanding",
                ].map((field) => (
                  <Table.Cell
                    key={field}
                    contentEditable={editingRow === i}
                    onBlur={(e) =>
                      updateCell(i, field, e.currentTarget.innerText)
                    }
                    onClick={() => setEditingRow(i)}
                    style={{
                      backgroundColor:
                        editingRow === i ? "#ffffd0" : "transparent",
                    }}
                  >
                    {row[field] ?? ""}
                  </Table.Cell>
                ))}
              </Table.Row>
            ))}
          </Table.Body>
        </Table>

        <Button
          positive
          icon="save"
          content="Save All Edits"
          onClick={saveWeeklyEdits}
          style={{ marginTop: "1rem" }}
        />
        <Button
          icon="refresh"
          content="Reload"
          onClick={loadWeeklyTable}
          style={{ marginTop: "1rem", marginLeft: "0.5rem" }}
        />
      </Segment>
    </div>
  );
}
