import React, { useState, useEffect } from "react";
import {
  Form,
  Button,
  Dropdown,
  Input,
  Table,
  Header,
  Checkbox,
  Modal,
  Message,
  Icon,
} from "semantic-ui-react";
import DivisionManager from "./DivisionManager";

const API = process.env.REACT_APP_API_URL || "";

export default function ManageStats() {
  // reference data
  const [users, setUsers] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [stats, setStats] = useState([]);

  // form state
  const [editId, setEditId] = useState(null);
  const [shortId, setShortId] = useState("");
  const [fullName, setFullName] = useState("");
  const [type, setType] = useState("personal");
  const [valueType, setValueType] = useState("number");
  const [reversed, setReversed] = useState(false);
  const [assignedUsers, setAssignedUsers] = useState([]);
  const [assignedDivs, setAssignedDivs] = useState([]);
  const [responsibleUser, setResponsibleUser] = useState(null);

  // UI state
  const [divisionModalOpen, setDivisionModalOpen] = useState(false);
  const [alertOpen, setAlertOpen] = useState(false);
  const [alertHeader, setAlertHeader] = useState("");
  const [alertMessage, setAlertMessage] = useState("");
  const [loading, setLoading] = useState(false);

  // helpers
  const showAlert = (header, message) => {
    setAlertHeader(header);
    setAlertMessage(String(message));
    setAlertOpen(true);
  };

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

  // initial load
  useEffect(() => {
    (async () => {
      const [u, d, s] = await Promise.all([
        fetchJSON(`${API}/api/users`, "Users"),
        fetchJSON(`${API}/api/divisions`, "Divisions"),
        fetchJSON(`${API}/api/stats/all`, "Stats"),
      ]);
      if (u) {
        setUsers(u.map((x) => ({ key: x.id, text: x.username, value: x.id })));
      }
      if (d) {
        setDivisions(d.map((x) => ({ key: x.id, text: x.name, value: x.id })));
      }
      if (s) {
        setStats(Array.isArray(s) ? s : []);
      }
    })();
  }, []);

  const refreshDivisions = async () => {
    try {
      const d = await fetch(`${API}/api/divisions`, { credentials: "include" }).then((r) => r.json());
      setDivisions(d.map((x) => ({ key: x.id, text: x.name, value: x.id })));
    } catch (err) {
      showAlert("Network Error", "Could not refresh divisions");
    }
  };

  const refreshStats = async () => {
    try {
      const s = await fetch(`${API}/api/stats/all`, { credentials: "include" }).then((r) => r.json());
      setStats(Array.isArray(s) ? s : []);
    } catch (err) {
      showAlert("Network Error", "Could not refresh stats");
    }
  };

  // create/update stat
  const resetForm = () => {
    setEditId(null);
    setShortId("");
    setFullName("");
    setType("personal");
    setValueType("number");
    setReversed(false);
    setAssignedUsers([]);
    setAssignedDivs([]);
    setResponsibleUser(null);
  };

  const startEdit = (stat) => {
    setEditId(stat.id);
    setShortId(stat.short_id || "");
    setFullName(stat.full_name || "");
    setType(stat.type || "personal");
    setValueType(stat.value_type || "number");
    setReversed(!!stat.reversed);
    setAssignedUsers(stat.user_ids || []);
    setAssignedDivs(stat.division_ids || []);
    // backend may or may not return responsible_user_id; handle both cases
    setResponsibleUser(stat.responsible_user_id ?? null);
  };

  const submitStat = async (e) => {
    e.preventDefault();

    if (!shortId.trim() || !fullName.trim()) {
      showAlert("Validation", "Short ID and Full Name are required");
      return;
    }

    // Build payload and only include optional fields if set
    const payload = {
      id: editId,
      short_id: shortId.trim().toUpperCase(),
      full_name: fullName.trim(),
      type,
      value_type: valueType,
      reversed: !!reversed,
      user_ids: Array.isArray(assignedUsers) ? assignedUsers : [],
      division_ids: type === "divisional" && Array.isArray(assignedDivs) ? assignedDivs : [],
    };
    if (responsibleUser !== null && responsibleUser !== undefined && responsibleUser !== "") {
      payload.responsible_user_id = responsibleUser;
    }

    setLoading(true);
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
        showAlert("Success", data.message || "Stat saved");
        resetForm();
        await refreshStats();
      } else {
        showAlert("Error", data.message || "Failed to save stat");
      }
    } catch (err) {
      console.error(err);
      showAlert("Network Error", "Could not save stat");
    } finally {
      setLoading(false);
    }
  };

  const deleteStat = async (id, name) => {
    if (!window.confirm(`Delete stat "${name}"?`)) return;
    try {
      const res = await fetch(`${API}/api/stats/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      const data = await res.json();
      if (res.ok) {
        showAlert("Success", data.message || "Stat deleted");
        setStats((s) => s.filter((st) => st.id !== id));
      } else {
        showAlert("Error", data.message || "Failed to delete stat");
      }
    } catch {
      showAlert("Network Error", "Could not delete stat");
    }
  };

  // map assigned display names safely (normalize id types to string)
  const mapAssignedNames = (stat) => {
    const assigned = [];
    if (stat.user_ids?.length) {
      assigned.push(
        ...stat.user_ids.map(
          (id) => users.find((u) => String(u.value) === String(id))?.text || id
        )
      );
    }
    if (stat.division_ids?.length) {
      assigned.push(
        ...stat.division_ids.map(
          (id) => divisions.find((d) => String(d.value) === String(id))?.text || id
        )
      );
    }
    return assigned.length ? assigned.join(", ") : "—";
  };

  return (
    <div className="ui container" style={{ marginTop: "2rem" }}>
      <Header as="h1" textAlign="center">
        Manage Stats
      </Header>

      {/* Create / Edit Form */}
      <div className="ui raised segment">
        <Header as="h3" dividing>
          {editId ? "Edit Stat" : "Create New Stat"}
        </Header>

        <Form onSubmit={submitStat} loading={loading}>
          <Form.Group widths="equal">
            <Form.Field required>
              <label>Short ID (e.g. GI)</label>
              <Input placeholder="GI" value={shortId} onChange={(e) => setShortId(e.target.value)} />
            </Form.Field>
            <Form.Field required>
              <label>Full Name</label>
              <Input placeholder="Gross Income" value={fullName} onChange={(e) => setFullName(e.target.value)} />
            </Form.Field>
          </Form.Group>

          <Form.Group widths="equal">
            <Form.Field required>
              <label>Type</label>
              <Dropdown
                selection
                options={[
                  { key: "personal", text: "Personal", value: "personal" },
                  { key: "divisional", text: "Divisional", value: "divisional" },
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
                  { key: "percentage", text: "Percentage (%)", value: "percentage" },
                ]}
                value={valueType}
                onChange={(_, { value }) => setValueType(value)}
              />
            </Form.Field>

            <Form.Field>
              <label>Reversed (upside-down)</label>
              <Checkbox toggle checked={reversed} onChange={(_, { checked }) => setReversed(!!checked)} />
            </Form.Field>
          </Form.Group>

          {/* Responsible is optional now */}
          <Form.Field>
            <label>Responsible</label>
            <Dropdown
              placeholder="Select responsible user (optional)"
              fluid
              selection
              options={users}
              value={responsibleUser}
              onChange={(_, { value }) => setResponsibleUser(value)}
              clearable
            />
          </Form.Field>

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

          {type === "divisional" && (
            <Form.Field>
              <label>
                Assign to Divisions{" "}
                <Button basic size="tiny" onClick={() => setDivisionModalOpen(true)} icon>
                  <Icon name="cogs" /> Manage
                </Button>
              </label>
              <Dropdown
                placeholder="Select divisions"
                fluid
                multiple
                selection
                options={divisions}
                value={assignedDivs}
                onChange={(_, { value }) => setAssignedDivs(value)}
              />
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

      {/* List of Stats */}
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
              <Table.HeaderCell>Responsible</Table.HeaderCell>
              <Table.HeaderCell>Assigned To</Table.HeaderCell>
              <Table.HeaderCell>Actions</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {stats.map((s) => (
              <Table.Row key={s.id}>
                <Table.Cell>
                  <strong>{s.short_id}</strong>
                </Table.Cell>
                <Table.Cell>{s.full_name}</Table.Cell>
                <Table.Cell>{s.type}</Table.Cell>
                <Table.Cell>{s.value_type}</Table.Cell>
                <Table.Cell>{s.reversed ? "Yes" : "No"}</Table.Cell>
                <Table.Cell>
                  {users.find((u) => String(u.value) === String(s.responsible_user_id))
                    ? users.find((u) => String(u.value) === String(s.responsible_user_id)).text
                    : s.responsible_user_id || "—"}
                </Table.Cell>
                <Table.Cell>{mapAssignedNames(s)}</Table.Cell>
                <Table.Cell>
                  <Button size="mini" icon="edit" onClick={() => startEdit(s)} />
                  <Button size="mini" icon="trash" negative onClick={() => deleteStat(s.id, s.full_name)} />
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </div>

      {/* Division Manager modal (react) */}
      <DivisionManager
        open={divisionModalOpen}
        onClose={async () => {
          setDivisionModalOpen(false);
          await refreshDivisions();
        }}
        divisions={divisions}
        refreshDivisions={refreshDivisions}
        showAlert={showAlert}
      />

      {/* Alert Modal */}
      <Modal open={alertOpen} onClose={() => setAlertOpen(false)} size="small">
        <Modal.Header>{alertHeader}</Modal.Header>
        <Modal.Content>
          <Message content={alertMessage} />
        </Modal.Content>
        <Modal.Actions>
          <Button onClick={() => setAlertOpen(false)}>OK</Button>
        </Modal.Actions>
      </Modal>
    </div>
  );
}
