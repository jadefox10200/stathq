import { useState } from "react";
import { Form, Button, Input, Message, Dropdown } from "semantic-ui-react";

export default function ManageUsers() {
  const [companyID, setCompanyID] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("user");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const roleOptions = [
    { key: "admin", text: "Admin", value: "admin" },
    { key: "user", text: "User", value: "user" },
  ];

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    try {
      const response = await fetch(`${process.env.REACT_APP_API_URL}/users`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          company_id: companyID,
          username,
          password,
          role,
        }),
        credentials: "include",
      });

      const data = await response.json();
      if (response.ok) {
        setSuccess("User created successfully");
        setCompanyID("");
        setUsername("");
        setPassword("");
        setRole("user");
      } else {
        setError(data.message || "User creation failed");
      }
    } catch (err) {
      setError("Server error");
    }
  };

  return (
    <div className="ui container">
      <h1 className="center-it">Stat HQ - Manage Users</h1>
      <Form onSubmit={handleSubmit}>
        <Form.Field>
          <label>Company ID</label>
          <Input
            value={companyID}
            onChange={(e) => setCompanyID(e.target.value)}
            placeholder="Enter company ID"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Username</label>
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Enter username"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Password</label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter password"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Role</label>
          <Dropdown
            placeholder="Select role"
            selection
            options={roleOptions}
            value={role}
            onChange={(e, { value }) => setRole(value)}
          />
        </Form.Field>
        {error && <Message negative>{error}</Message>}
        {success && <Message positive>{success}</Message>}
        <Button type="submit" primary>
          Create User
        </Button>
      </Form>
    </div>
  );
}
