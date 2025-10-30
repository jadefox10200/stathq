import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Form, Button, Input, Message } from "semantic-ui-react";

export default function Register() {
  const [companyID, setCompanyID] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");

    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/register`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            company_id: companyID,
            company_name: companyName,
            username,
            password,
          }),
          credentials: "include",
        }
      );

      const data = await response.json();
      if (response.ok) {
        navigate("/login");
      } else {
        setError(data.message || "Registration failed");
      }
    } catch (err) {
      setError("Server error");
    }
  };

  return (
    <div className="ui container">
      <h1 className="center-it">Stat HQ - Register Company</h1>
      <Form onSubmit={handleSubmit}>
        <Form.Field>
          <label>Company ID</label>
          <Input
            value={companyID}
            onChange={(e) => setCompanyID(e.target.value)}
            placeholder="Enter unique company ID"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Company Name</label>
          <Input
            value={companyName}
            onChange={(e) => setCompanyName(e.target.value)}
            placeholder="Enter company name"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Admin Username</label>
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Enter admin username"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Admin Password</label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter admin password"
            required
          />
        </Form.Field>
        {error && <Message negative>{error}</Message>}
        <Button type="submit" primary>
          Register
        </Button>
      </Form>
    </div>
  );
}
