// src/pages/ManageStats.js
import { useState, useEffect } from "react";
import {
  Form,
  Button,
  Dropdown,
  Input,
  Table,
  Header,
  Icon,
  Checkbox,
} from "semantic-ui-react";

const API = process.env.REACT_APP_API_URL || "";

export default function ManageStats() {
  // === Global Modal & Alert ===
  const showAlert = (header, message) => {
    $("#alertHeader").text(header);
    $("#alertMessage").html(
      String(message)
        .replace(/\n/g, "<br>")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
    );
    $("#alertModal").modal("show");
  };

  // === Reference Data ===
  const [users, setUsers] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [stats, setStats] = useState([]);

  // === Form State ===
  const [editId, setEditId] = useState(null);
  const [shortId, setShortId] = useState("");
  const [fullName, setFullName] = useState("");
  const [type, setType] = useState("personal");
  const [valueType, setValueType] = useState("number");
  const [reversed, setReversed] = useState(false);
  const [assignedUsers, setAssignedUsers] = useState([]);
  const [assignedDivs, setAssignedDivs] = useState([]);

  // === Division Modal ===
  const [newDivName, setNewDivName] = useState("");

  // === Load Data ===
  useEffect(() => {
    // Initialize modal
    $("#alertModal").modal({
      closable: true,
      onApprove: () => false,
    });

    $(document)
      .off("click", "#okButton")
      .on("click", "#okButton", () => {
        $("#alertModal").modal("hide");
      });

    const fetchJSON = async (url, name) => {
      try {
        const res = await fetch(url, { credentials: "include" });
        const text = await res.text();
        if (!res.ok) {
          let msg = `Failed to load ${name.toLowerCase()}`;
          try {
            const err = JSON.parse(text);
            msg = err.message || msg;
            if (err.details) console.error(`${name} details:`, err.details);
          } catch {
            console.error(`${name} raw error:`, text);
          }
          throw new Error(msg);
        }
        return JSON.parse(text);
      } catch (err) {
        showAlert("Load Error", err.message);
        return null;
      }
    };

    Promise.all([
      fetchJSON(`${API}/api/users`, "Users"),
      fetchJSON(`${API}/api/divisions`, "Divisions"),
      fetchJSON(`${API}/api/stats/all`, "Stats"),
    ]).then(([u, d, s]) => {
      if (u) {
        setUsers(u.map((x) => ({ key: x.id, text: x.username, value: x.id })));
      }
      if (d) {
        setDivisions(d.map((x) => ({ key: x.id, text: x.name, value: x.id })));
      }
      if (s) {
        setStats(Array.isArray(s) ? s : []);
      }
    });
  }, []);

  // === Refresh Divisions ===
  const refreshDivisions = async () => {
    const d = await fetch(`${API}/api/divisions`, {
      credentials: "include",
    }).then((r) => r.json());
    setDivisions(d.map((x) => ({ key: x.id, text: x.name, value: x.id })));
  };

  // === Create Division ===
  const createDivision = async () => {
    const name = newDivName.trim();
    if (!name) {
      showAlert("Invalid Input", "Division name is required");
      return;
    }
    try {
      const res = await fetch(`${API}/api/divisions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ name }),
      });
      const data = await res.json();
      if (res.ok) {
        setNewDivName("");
        refreshDivisions();
        showAlert("Success", data.message);
      } else {
        showAlert("Error", data.message);
      }
    } catch {
      showAlert("Network Error", "Could not create division");
    }
  };

  // === Delete Division ===
  const deleteDivision = async (id, name) => {
    if (!window.confirm(`Delete division "${name}"?`)) return;
    try {
      const res = await fetch(`${API}/api/divisions/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      const data = await res.json();
      if (res.ok) {
        refreshDivisions();
        showAlert("Success", data.message);
      } else {
        showAlert("Error", data.message);
      }
    } catch {
      showAlert("Network Error", "Could not delete division");
    }
  };

  // === Form Helpers ===
  const resetForm = () => {
    setEditId(null);
    setShortId("");
    setFullName("");
    setType("personal");
    setValueType("number");
    setReversed(false);
    setAssignedUsers([]);
    setAssignedDivs([]);
  };

  const startEdit = (stat) => {
    setEditId(stat.id);
    setShortId(stat.short_id);
    setFullName(stat.full_name);
    setType(stat.type);
    setValueType(stat.value_type);
    setReversed(stat.reversed);
    setAssignedUsers(stat.user_ids || []);
    setAssignedDivs(stat.division_ids || []);
  };

  const submitStat = async (e) => {
    e.preventDefault();
    if (!shortId.trim() || !fullName.trim()) {
      showAlert("Validation", "Short ID and Full Name are required");
      return;
    }

    const payload = {
      id: editId,
      short_id: shortId.trim().toUpperCase(),
      full_name: fullName.trim(),
      type,
      value_type: valueType,
      reversed,
      user_ids: type === "personal" ? assignedUsers : [],
      division_ids: type === "divisional" ? assignedDivs : [],
    };

    try {
      const url = editId ? `${API}/api/stats/${editId}` : `${API}/api/stats`;
      const method = editId ? "PATCH" : "POST";
      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (res.ok) {
        showAlert("Success", data.message);
        resetForm();
        const s = await fetch(`${API}/api/stats/all`, {
          credentials: "include",
        }).then((r) => r.json());
        setStats(s);
      } else {
        showAlert("Error", data.message);
      }
    } catch {
      showAlert("Network Error", "Could not save stat");
    }
  };

  // === Delete Stat ===
  const deleteStat = async (id, name) => {
    if (!window.confirm(`Delete stat "${name}"?`)) return;
    try {
      const res = await fetch(`${API}/api/stats/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      const data = await res.json();
      if (res.ok) {
        showAlert("Success", data.message);
        setStats(stats.filter((s) => s.id !== id));
      } else {
        showAlert("Error", data.message);
      }
    } catch {
      showAlert("Network Error", "Could not delete stat");
    }
  };

  // === Render ===
  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center">
        Manage Stats
      </Header>

      {/* === CREATE / EDIT FORM === */}
      <div className="ui raised segment">
        <Header as="h3" dividing>
          {editId ? "Edit Stat" : "Create New Stat"}
        </Header>

        <Form onSubmit={submitStat}>
          <Form.Group widths="equal">
            <Form.Field required>
              <label>Short ID (e.g. GI)</label>
              <Input
                placeholder="GI"
                value={shortId}
                onChange={(e) => setShortId(e.target.value)}
              />
            </Form.Field>
            <Form.Field required>
              <label>Full Name</label>
              <Input
                placeholder="Gross Income"
                value={fullName}
                onChange={(e) => setFullName(e.target.value)}
              />
            </Form.Field>
          </Form.Group>

          <Form.Group widths="equal">
            <Form.Field required>
              <label>Type</label>
              <Dropdown
                selection
                options={[
                  { key: "personal", text: "Personal", value: "personal" },
                  {
                    key: "divisional",
                    text: "Divisional",
                    value: "divisional",
                  },
                  { key: "main", text: "Main (Company)", value: "main" },
                ]}
                value={type}
                onChange={(_, { value }) => setType(value)}
              />
            </Form.Field>

            <Form.Field required>
              <label>Value Type</label>
              <Dropdown
                selection
                options={[
                  { key: "number", text: "Number", value: "number" },
                  { key: "currency", text: "Currency ($)", value: "currency" },
                  {
                    key: "percentage",
                    text: "Percentage (%)",
                    value: "percentage",
                  },
                ]}
                value={valueType}
                onChange={(_, { value }) => setValueType(value)}
              />
            </Form.Field>
          </Form.Group>

          <Form.Field>
            <Checkbox
              label="Reversed (upside-down)"
              checked={reversed}
              onChange={(_, { checked }) => setReversed(checked)}
            />
          </Form.Field>

          {type === "personal" && (
            <Form.Field>
              <label>Assign to Users</label>
              <Dropdown
                placeholder="Select users"
                fluid
                multiple
                selection
                options={users}
                value={assignedUsers}
                onChange={(_, { value }) => setAssignedUsers(value)}
              />
            </Form.Field>
          )}

          {type === "divisional" && (
            <Form.Field>
              <label>Assign to Divisions</label>
              <div
                style={{ display: "flex", gap: "0.5rem", alignItems: "end" }}
              >
                <Dropdown
                  placeholder="Select divisions"
                  fluid
                  multiple
                  selection
                  options={divisions}
                  value={assignedDivs}
                  onChange={(_, { value }) => setAssignedDivs(value)}
                  style={{ flex: 1 }}
                />
                <Button
                  type="button"
                  icon="cogs"
                  content="Manage"
                  onClick={() => {
                    setNewDivName("");
                    $("#alertModal .header").text("Manage Divisions");
                    $("#alertModal .content").html(`
                      <div class="ui form">
                        <div class="field">
                          <label>New Division</label>
                          <input type="text" id="newDivInput" placeholder="Enter name" value="" />
                        </div>
                        <button class="ui primary button" id="addDivBtn">Add Division</button>
                      </div>
                      <div style="margin-top:1rem;">
                        <strong>Existing Divisions:</strong>
                        <div id="divList" style="max-height:200px; overflow-y:auto; margin-top:0.5rem;"></div>
                      </div>
                    `);

                    // Populate list
                    const list = divisions.length
                      ? divisions
                          .map(
                            (d) => `
                          <div style="display:flex; justify-content:space-between; margin:0.3rem 0; padding:0.3rem; border-bottom:1px solid #eee;">
                            <span>${d.text}</span>
                            <i class="trash icon red link" data-id="${d.value}" style="cursor:pointer;"></i>
                          </div>
                        `
                          )
                          .join("")
                      : "<em style='color:#999;'>No divisions yet</em>";
                    $("#divList").html(list);

                    // Bind events
                    $(document).on("click", "#addDivBtn", () => {
                      const val = $("#newDivInput").val().trim();
                      console.log("newDiv: ", val);
                      if (val) {
                        setNewDivName(val);
                        createDivision();
                        $("#alertModal").modal("hide");
                      }
                    });

                    $(document)
                      .off("click", ".trash.icon")
                      .on("click", ".trash.icon", function () {
                        const id = $(this).data("id");
                        const name = $(this).parent().find("span").text();
                        deleteDivision(id, name);
                        $("#alertModal").modal("hide");
                      });

                    $("#alertModal").modal("show");
                  }}
                />
              </div>
            </Form.Field>
          )}

          <Button primary type="submit">
            {editId ? "Update Stat" : "Create Stat"}
          </Button>
          {editId && (
            <Button type="button" onClick={resetForm}>
              Cancel
            </Button>
          )}
        </Form>
      </div>

      {/* === LIST OF STATS === */}
      <div className="ui raised segment" style={{ marginTop: "2rem" }}>
        <Header as="h3" dividing>
          Existing Stats
        </Header>
        <Table celled structured>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>Short ID</Table.HeaderCell>
              <Table.HeaderCell>Full Name</Table.HeaderCell>
              <Table.HeaderCell>Type</Table.HeaderCell>
              <Table.HeaderCell>Value Type</Table.HeaderCell>
              <Table.HeaderCell>Reversed</Table.HeaderCell>
              <Table.HeaderCell>Assigned To</Table.HeaderCell>
              <Table.HeaderCell>Actions</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {stats.map((s) => {
              const assigned = [];
              if (s.user_ids?.length)
                assigned.push(
                  ...s.user_ids.map(
                    (id) => users.find((u) => u.value === id)?.text || id
                  )
                );
              if (s.division_ids?.length)
                assigned.push(
                  ...s.division_ids.map(
                    (id) => divisions.find((d) => d.value === id)?.text || id
                  )
                );
              return (
                <Table.Row key={s.id}>
                  <Table.Cell>
                    <strong>{s.short_id}</strong>
                  </Table.Cell>
                  <Table.Cell>{s.full_name}</Table.Cell>
                  <Table.Cell>{s.type}</Table.Cell>
                  <Table.Cell>{s.value_type}</Table.Cell>
                  <Table.Cell>{s.reversed ? "Yes" : "No"}</Table.Cell>
                  <Table.Cell>
                    {assigned.length ? assigned.join(", ") : "—"}
                  </Table.Cell>
                  <Table.Cell>
                    <Button
                      size="mini"
                      icon="edit"
                      onClick={() => startEdit(s)}
                    />
                    <Button
                      size="mini"
                      icon="trash"
                      negative
                      onClick={() => deleteStat(s.id, s.full_name)}
                    />
                  </Table.Cell>
                </Table.Row>
              );
            })}
          </Table.Body>
        </Table>
      </div>

      {/* === GLOBAL MODAL === */}
      <div className="ui modal" id="alertModal">
        <div className="header" id="alertHeader">
          Alert
        </div>
        <div className="content">
          <p id="alertMessage"></p>
        </div>
        <div className="actions">
          <div className="ui button" id="okButton">
            OK
          </div>
        </div>
      </div>
    </div>
  );
}
