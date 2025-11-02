import React, { useEffect, useState } from "react";
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

  // For editing
  const [editingId, setEditingId] = useState(null);
  const [editName, setEditName] = useState("");

  // Clear and focus input when modal opens
  useEffect(() => {
    if (open) {
      setNewName("");
      // autofocus handled by prop on the Input element
    }
  }, [open]);

  const createDivision = async (nameParam) => {
    const name = (
      typeof nameParam !== "undefined" ? nameParam : newName
    ).trim();
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

  const startEdit = (id, name) => {
    setEditingId(id);
    setEditName(name);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditName("");
  };

  const saveEdit = async (id) => {
    const name = editName.trim();
    if (!name) {
      showAlert?.("Validation", "Division name is required");
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API}/api/divisions/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ name }),
      });
      const data = await res.json();
      if (res.ok) {
        await refreshDivisions();
        showAlert?.("Success", data.message || "Division updated");
      } else {
        showAlert?.("Error", data.message || "Failed to update division");
      }
    } catch (err) {
      console.error(err);
      showAlert?.("Network Error", "Could not update division");
    } finally {
      setLoading(false);
      setEditingId(null);
      setEditName("");
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
    <Modal
      open={open}
      onClose={onClose}
      size="small"
      closeOnDimmerClick={!loading}
    >
      <Modal.Header>Manage Divisions</Modal.Header>
      <Modal.Content>
        <div
          style={{
            display: "flex",
            gap: "0.5rem",
            marginBottom: "1rem",
            alignItems: "center",
          }}
        >
          {/* Give the Input flex:1 so it expands within the flex row, autofocus for UX,
              prevent Enter from submitting outer forms by handling keyDown: */}
          <Input
            placeholder="New division name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            fluid
            style={{ flex: 1 }}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault(); // avoid submitting any outer form
                createDivision(newName);
              }
            }}
          />
          <Button
            primary
            type="button" // ensure this button does NOT submit any parent form
            onClick={() => createDivision(newName)}
            loading={loading}
          >
            Add
          </Button>
        </div>

        <List divided relaxed>
          {divisions.length ? (
            divisions.map((d) => (
              <List.Item key={d.key}>
                <List.Content floated="right">
                  {editingId === d.value ? (
                    <>
                      <Button
                        icon
                        size="mini"
                        onClick={() => saveEdit(d.value)}
                      >
                        <Icon name="check" />
                      </Button>
                      <Button icon size="mini" onClick={cancelEdit}>
                        <Icon name="cancel" />
                      </Button>
                    </>
                  ) : (
                    <>
                      <Button
                        icon
                        size="mini"
                        onClick={() => startEdit(d.value, d.text)}
                      >
                        <Icon name="edit" />
                      </Button>
                      <Button
                        icon
                        size="mini"
                        negative
                        onClick={() => askDelete({ id: d.value, name: d.text })}
                      >
                        <Icon name="trash" />
                      </Button>
                    </>
                  )}
                </List.Content>
                <List.Content>
                  {editingId === d.value ? (
                    <Input
                      value={editName}
                      onChange={(e) => setEditName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") saveEdit(d.value);
                        if (e.key === "Escape") cancelEdit();
                      }}
                    />
                  ) : (
                    d.text
                  )}
                </List.Content>
              </List.Item>
            ))
          ) : (
            <div style={{ color: "#999" }}>No divisions yet</div>
          )}
        </List>
      </Modal.Content>
      <Modal.Actions>
        <Button type="button" onClick={onClose} disabled={loading}>
          Close
        </Button>
      </Modal.Actions>

      <Confirm
        open={confirmOpen}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={doDelete}
        content={
          pendingDelete
            ? `Delete division "${pendingDelete.name}"?`
            : "Delete division?"
        }
      />
    </Modal>
  );
}
