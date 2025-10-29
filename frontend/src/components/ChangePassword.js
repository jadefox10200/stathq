import { useState } from "react";
import { Form, Button, Message } from "semantic-ui-react";
import { useNavigate } from "react-router-dom";

export default function ChangePassword() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmNewPassword, setConfirmNewPassword] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    // Validate that newPassword and confirmNewPassword match
    if (newPassword !== confirmNewPassword) {
      setError("New passwords do not match");
      return;
    }

    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/api/change-password`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            old_password: oldPassword,
            new_password: newPassword,
          }),
          credentials: "include",
        }
      );
      const data = await response.json();
      if (response.ok) {
        setSuccess(data.message);
        setOldPassword("");
        setNewPassword("");
        setConfirmNewPassword("");
        setTimeout(() => navigate("/"), 3000); // Redirect to home after 3 seconds
      } else {
        setError(data.message || "Failed to change password");
      }
    } catch (err) {
      setError(err.message);
    }
  };

  return (
    <div className="ui container">
      <h1 className="center-it">Change Password</h1>
      <Form onSubmit={handleSubmit}>
        <Form.Field>
          <label>Old Password</label>
          <Form.Input
            type="password"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            placeholder="Enter old password"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>New Password</label>
          <Form.Input
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="Enter new password"
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Confirm New Password</label>
          <Form.Input
            type="password"
            value={confirmNewPassword}
            onChange={(e) => setConfirmNewPassword(e.target.value)}
            placeholder="Confirm new password"
            required
          />
        </Form.Field>
        {error && <Message negative>{error}</Message>}
        {success && <Message positive>{success}</Message>}
        <Button type="submit" primary>
          Change Password
        </Button>
      </Form>
    </div>
  );
}
