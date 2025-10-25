import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Form, Button, Input, Message } from "semantic-ui-react";

export default function Login() {
  const [companyID, setCompanyID] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");

    try {
      const response = await fetch(`${process.env.REACT_APP_API_URL}/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ company_id: companyID, username, password }),
        credentials: "include",
      });

      const data = await response.json();
      if (response.ok) {
        navigate("/");
      } else {
        setError(data.message || "Invalid credentials");
      }
    } catch (err) {
      setError("Server error");
    }
  };

  return (
    <div className="ui container">
      <h1 className="center-it">Stat HQ Login</h1>
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
        {error && <Message negative>{error}</Message>}
        <Button type="submit" primary>
          Login
        </Button>
      </Form>
    </div>
  );
}
