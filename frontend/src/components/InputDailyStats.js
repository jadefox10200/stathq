import { useState, useEffect } from "react";
import { Form, Button, Dropdown, Input } from "semantic-ui-react";

export default function InputDailyStats() {
  const [stats, setStats] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [selectedStat, setSelectedStat] = useState("");
  const [selectedDivision, setSelectedDivision] = useState("");
  const [date, setDate] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  useEffect(() => {
    // Fetch stats
    fetch(`${process.env.REACT_APP_API_URL}/api/stats`, {
      credentials: "include",
    })
      .then((res) => res.json())
      .then((data) =>
        setStats(data.map((s) => ({ key: s.id, text: s.name, value: s.id })))
      )
      .catch((err) => setError("Failed to load stats"));

    // Fetch divisions
    fetch(`${process.env.REACT_APP_API_URL}/api/divisions`, {
      credentials: "include",
    })
      .then((res) => res.json())
      .then((data) =>
        setDivisions(
          data.map((d) => ({ key: d.id, text: d.name, value: d.id }))
        )
      )
      .catch((err) => setError("Failed to load divisions"));
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    try {
      const response = await fetch(
        `${process.env.REACT_APP_API_URL}/services/save7R`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            stat_id: selectedStat,
            date,
            value: parseInt(value),
            division_id: selectedDivision,
          }),
          credentials: "include",
        }
      );

      if (response.ok) {
        setSuccess("Daily stat saved");
        setSelectedStat("");
        setSelectedDivision("");
        setDate("");
        setValue("");
      } else {
        const data = await response.json();
        setError(data.message || "Failed to save stat");
      }
    } catch (err) {
      setError("Server error");
    }
  };

  return (
    <div className="ui container">
      <h1 className="center-it">Stat HQ - Input Daily Stats</h1>
      <Form onSubmit={handleSubmit}>
        <Form.Field>
          <label>Stat</label>
          <Dropdown
            placeholder="Select stat"
            selection
            options={stats}
            value={selectedStat}
            onChange={(e, { value }) => setSelectedStat(value)}
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Division</label>
          <Dropdown
            placeholder="Select division"
            selection
            options={divisions}
            value={selectedDivision}
            onChange={(e, { value }) => setSelectedDivision(value)}
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Date</label>
          <Input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            required
          />
        </Form.Field>
        <Form.Field>
          <label>Value</label>
          <Input
            type="number"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            required
          />
        </Form.Field>
        {error && (
          <Form.Field>
            <Message negative>{error}</Message>
          </Form.Field>
        )}
        {success && (
          <Form.Field>
            <Message positive>{success}</Message>
          </Form.Field>
        )}
        <Button type="submit" primary>
          Save
        </Button>
      </Form>
    </div>
  );
}
