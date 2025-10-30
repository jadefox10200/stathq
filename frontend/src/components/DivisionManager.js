import React, { useState } from "react";
import { Modal, Button, Input, List, Icon, Confirm } from "semantic-ui-react";

const API = process.env.REACT_APP_API_URL || "";

export default function DivisionManager({
  open,
  onClose,
  divisions = [],
  refreshDivisions,
  showAlert,
}) {
  const [newName, setNewName] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState(null);
  const [loading, setLoading] = useState(false);

  const createDivision = async (nameParam) => {
    const name = (typeof nameParam !== "undefined" ? nameParam : newName).trim();
    if (!name) {
      showAlert?.("Validation", "Division name is required");
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API}/api/divisions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ name }),
      });
      const data = await res.json();
      if (res.ok) {
        setNewName("");
        await refreshDivisions();
        showAlert?.("Success", data.message || "Division created");
      } else {
        showAlert?.("Error", data.message || "Failed to create division");
      }
    } catch (err) {
      console.error(err);
      showAlert?.("Network Error", "Could not create division");
    } finally {
      setLoading(false);
    }
  };

  const askDelete = (division) => {
    setPendingDelete(division);
    setConfirmOpen(true);
  };

  const doDelete = async () => {
    if (!pendingDelete) return;
    setLoading(true);
    try {
      const res = await fetch(`${API}/api/divisions/${pendingDelete.id}`, {
        method: "DELETE",
        credentials: "include",
      });
      const data = await res.json();
      if (res.ok) {
        showAlert?.("Success", data.message || "Division deleted");
        await refreshDivisions();
      } else {
        showAlert?.("Error", data.message || "Failed to delete division");
      }
    } catch (err) {
      console.error(err);
      showAlert?.("Network Error", "Could not delete division");
    } finally {
      setLoading(false);
      setConfirmOpen(false);
      setPendingDelete(null);
    }
  };

  return (
    <Modal open={open} onClose={onClose} size="small">
      <Modal.Header>Manage Divisions</Modal.Header>
      <Modal.Content>
        <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem" }}>
          <Input placeholder="New division name" value={newName} onChange={(e) => setNewName(e.target.value)} fluid />
          <Button primary onClick={() => createDivision(newName)} loading={loading}>
            Add
          </Button>
        </div>

        <List divided relaxed>
          {divisions.length ? (
            divisions.map((d) => (
              <List.Item key={d.key}>
                <List.Content floated="right">
                  <Button icon size="mini" negative onClick={() => askDelete({ id: d.value, name: d.text })}>
                    <Icon name="trash" />
                  </Button>
                </List.Content>
                <List.Content>{d.text}</List.Content>
              </List.Item>
            ))
          ) : (
            <div style={{ color: "#999" }}>No divisions yet</div>
          )}
        </List>
      </Modal.Content>
      <Modal.Actions>
        <Button onClick={onClose}>Close</Button>
      </Modal.Actions>

      <Confirm
        open={confirmOpen}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={doDelete}
        content={pendingDelete ? `Delete division "${pendingDelete.name}"?` : "Delete division?"}
      />
    </Modal>
  );
}
