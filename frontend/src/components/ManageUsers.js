import { useState, useEffect } from "react";
import { Form, Button, Message, Table, Dropdown } from "semantic-ui-react";
import AlertModal from "./AlertModal";

export default function ManageUsers() {
  const [users, setUsers] = useState([]);
  const [companyId, setCompanyId] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("user");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [resetPasswordUserId, setResetPasswordUserId] = useState(null);
  const [newPassword, setNewPassword] = useState("");

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

  const handleDeleteUser = async (userId) => {
    setError("");
    setSuccess("");
    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/api/users/${userId}`,
        {
          method: "DELETE",
          credentials: "include",
        }
      );
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setUsers(users.filter((user) => user.id !== userId));
      } else {
        setError(data.message || `Failed to delete user: ${response.status}`);
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

  const handleUpdateRole = async (userId, newRole) => {
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
      } else {
        setError(data.message || `Failed to update role: ${response.status}`);
      }
    } catch (err) {
      setError(err.message || "An error occurred. Please try again.");
    }
  };

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
              options={[
                { key: "user", text: "User", value: "user" },
                { key: "admin", text: "Admin", value: "admin" },
              ]}
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
                  options={[
                    { key: "user", text: "User", value: "user" },
                    { key: "admin", text: "Admin", value: "admin" },
                  ]}
                  value={user.role}
                  onChange={(e, { value }) => handleUpdateRole(user.id, value)}
                />
              </Table.Cell>
              <Table.Cell>
                <Button
                  secondary
                  onClick={() => setResetPasswordUserId(user.id)}
                >
                  Reset Password
                </Button>
                <Button negative onClick={() => handleDeleteUser(user.id)}>
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
      <AlertModal />
    </div>
  );
}
