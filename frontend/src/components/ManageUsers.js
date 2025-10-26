import { useState, useEffect } from "react";
import {
  Form,
  Button,
  Message,
  Table,
  Dropdown,
  Modal,
  Header,
} from "semantic-ui-react";

export default function ManageUsers() {
  const [users, setUsers] = useState([]);
  const [companyId, setCompanyId] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("user");
  const [changedRoles, setChangedRoles] = useState({}); // Track pending role changes { userId: newRole }
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [resetPasswordUserId, setResetPasswordUserId] = useState(null);
  const [newPassword, setNewPassword] = useState("");
  const [modalOpen, setModalOpen] = useState(false);
  const [userToDelete, setUserToDelete] = useState(null); // Track user for deletion

  useEffect(() => {
    // Fetch current user's company_id
    fetch(`${process.env.REACT_APP_API_URL}/api/user`, {
      credentials: "include",
    })
      .then(async (res) => {
        if (!res.ok) {
          const text = await res.text();
          throw new Error(`Failed to fetch user info: ${res.status} ${text}`);
        }
        return res.json();
      })
      .then((data) => {
        setCompanyId(data.company_id);
        // Fetch users for the company
        return fetch(`${process.env.REACT_APP_API_URL}/api/users`, {
          credentials: "include",
        });
      })
      .then(async (res) => {
        if (!res.ok) {
          const text = await res.text();
          throw new Error(`Failed to fetch users: ${res.status} ${text}`);
        }
        return res.json();
      })
      .then(setUsers)
      .catch((err) =>
        setError(err.message || "An error occurred while fetching users")
      );
  }, []);

  const handleCreateUser = async () => {
    setError("");
    setSuccess("");
    try {
      const response = await fetch(`${process.env.REACT_APP_API_URL}/users`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          company_id: companyId,
          username,
          password,
          role,
        }),
        credentials: "include",
      });
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setUsername("");
        setPassword("");
        setRole("user");
        setShowCreateForm(false);
        // Refresh user list
        const res = await fetch(`${process.env.REACT_APP_API_URL}/api/users`, {
          credentials: "include",
        });
        if (!res.ok) {
          const text = await res.text();
          throw new Error(`Failed to refresh user list: ${res.status} ${text}`);
        }
        setUsers(await res.json());
      } else {
        setError(data.message || `Failed to create user: ${response.status}`);
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

  const handleResetPassword = async (userId) => {
    setError("");
    setSuccess("");
    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/api/users/reset-password`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ user_id: userId, new_password: newPassword }),
          credentials: "include",
        }
      );
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setResetPasswordUserId(null);
        setNewPassword("");
      } else {
        setError(
          data.message || `Failed to reset password: ${response.status}`
        );
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

  const handleDeleteUser = async (userId, username) => {
    setUserToDelete({ id: userId, username });
    setModalOpen(true);
  };

  const confirmDelete = async () => {
    if (!userToDelete) return;
    setError("");
    setSuccess("");
    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/api/users/${userToDelete.id}`,
        {
          method: "DELETE",
          credentials: "include",
        }
      );
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setUsers(users.filter((user) => user.id !== userToDelete.id));
        setModalOpen(false);
        setUserToDelete(null);
      } else {
        setError(data.message || `Failed to delete user: ${response.status}`);
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

  const handleUpdateRole = async (userId) => {
    const newRole = changedRoles[userId];
    if (!newRole) return;
    setError("");
    setSuccess("");
    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/api/users/${userId}/role`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ role: newRole }),
          credentials: "include",
        }
      );
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setUsers(
          users.map((user) =>
            user.id === userId ? { ...user, role: newRole } : user
          )
        );
        setChangedRoles((prev) => {
          const updated = { ...prev };
          delete updated[userId];
          return updated;
        });
      } else {
        setError(data.message || `Failed to update role: ${response.status}`);
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

  const handleRoleChange = (userId, newRole) => {
    setChangedRoles((prev) => ({
      ...prev,
      [userId]: newRole,
    }));
  };

  const roleOptions = [
    { key: "user", text: "User", value: "user" },
    { key: "admin", text: "Admin", value: "admin" },
  ];

  return (
    <div className="ui container">
      <h2>Manage Users</h2>
      {error && <Message negative>{error}</Message>}
      {success && <Message positive>{success}</Message>}
      <Button primary onClick={() => setShowCreateForm(!showCreateForm)}>
        {showCreateForm ? "Cancel" : "Create New User"}
      </Button>
      {showCreateForm && (
        <Form onSubmit={handleCreateUser} style={{ marginTop: "20px" }}>
          <Form.Field>
            <label>Company ID</label>
            <input type="text" value={companyId} readOnly />
          </Form.Field>
          <Form.Field>
            <label>Username</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="Enter username"
            />
          </Form.Field>
          <Form.Field>
            <label>Password</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter password"
            />
          </Form.Field>
          <Form.Field>
            <label>Role</label>
            <Dropdown
              selection
              options={roleOptions}
              value={role}
              onChange={(e, { value }) => setRole(value)}
            />
          </Form.Field>
          <Button type="submit" primary>
            Create User
          </Button>
        </Form>
      )}
      <Table celled style={{ marginTop: "20px" }}>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>Username</Table.HeaderCell>
            <Table.HeaderCell>Role</Table.HeaderCell>
            <Table.HeaderCell>Actions</Table.HeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {users.map((user) => (
            <Table.Row key={user.id}>
              <Table.Cell>{user.username}</Table.Cell>
              <Table.Cell>
                <Dropdown
                  selection
                  options={roleOptions}
                  value={changedRoles[user.id] || user.role}
                  onChange={(e, { value }) => handleRoleChange(user.id, value)}
                />
                <Button
                  style={{
                    marginLeft: "10px",
                    backgroundColor:
                      changedRoles[user.id] &&
                      changedRoles[user.id] !== user.role
                        ? "#21ba45" // Green when changed
                        : "#d3d3d3", // Grey when unchanged
                    color: "white",
                  }}
                  disabled={
                    !changedRoles[user.id] ||
                    changedRoles[user.id] === user.role
                  }
                  onClick={() => handleUpdateRole(user.id)}
                >
                  Save Role
                </Button>
              </Table.Cell>
              <Table.Cell>
                <Button
                  secondary
                  onClick={() => setResetPasswordUserId(user.id)}
                >
                  Reset Password
                </Button>
                <Button
                  negative
                  onClick={() => handleDeleteUser(user.id, user.username)}
                >
                  Delete
                </Button>
                {resetPasswordUserId === user.id && (
                  <Form onSubmit={() => handleResetPassword(user.id)}>
                    <Form.Field>
                      <input
                        type="password"
                        value={newPassword}
                        onChange={(e) => setNewPassword(e.target.value)}
                        placeholder="New password"
                      />
                    </Form.Field>
                    <Button primary type="submit">
                      Submit
                    </Button>
                    <Button
                      secondary
                      onClick={() => setResetPasswordUserId(null)}
                    >
                      Cancel
                    </Button>
                  </Form>
                )}
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
      <Modal
        open={modalOpen}
        onClose={() => {
          setModalOpen(false);
          setUserToDelete(null);
        }}
        size="small"
      >
        <Header content="Confirm Deletion" />
        <Modal.Content>
          <p>
            Are you sure you want to delete user{" "}
            <strong>{userToDelete?.username}</strong>?
          </p>
        </Modal.Content>
        <Modal.Actions>
          <Button
            onClick={() => {
              setModalOpen(false);
              setUserToDelete(null);
            }}
          >
            Cancel
          </Button>
          <Button negative onClick={confirmDelete}>
            Delete
          </Button>
        </Modal.Actions>
      </Modal>
    </div>
  );
}
