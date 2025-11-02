import React from "react";
import { Button, Dropdown } from "semantic-ui-react";

/**
 * Props:
 * - filterType: "all" | "user" | "division"
 * - onFilterTypeChange(type)
 * - selectedUser: id|null
 * - onSelectedUserChange(id|null)
 * - selectedDivision: id|null
 * - onSelectedDivisionChange(id|null)
 * - userOptions: [{ key, text, value }]
 * - divisionOptions: [{ key, text, value }]
 * - showClearButtons?: boolean (default true)
 * - compact?: boolean (reduce spacing)
 */
export default function ScopeFilter({
  filterType,
  onFilterTypeChange,
  selectedUser,
  onSelectedUserChange,
  selectedDivision,
  onSelectedDivisionChange,
  userOptions = [],
  divisionOptions = [],
  showClearButtons = true,
  compact = false,
}) {
  const gap = compact ? 6 : 8;
  return (
    <div>
      <label>Scope / Filter</label>
      <div style={{ display: "flex", gap, alignItems: "center" }}>
        <Button
          toggle
          active={filterType === "user"}
          onClick={() => {
            onFilterTypeChange("user");
            onSelectedDivisionChange(null);
          }}
        >
          Personal
        </Button>
        <Button
          toggle
          active={filterType === "division"}
          onClick={() => {
            onFilterTypeChange("division");
            onSelectedUserChange(null);
          }}
        >
          Division
        </Button>
        <Button
          toggle
          active={filterType === "all"}
          onClick={() => {
            onFilterTypeChange("all");
            onSelectedUserChange(null);
            onSelectedDivisionChange(null);
          }}
        >
          All
        </Button>
      </div>

      {filterType === "user" && (
        <div style={{ display: "flex", marginTop: gap, gap }}>
          <Dropdown
            placeholder="Select user"
            search
            selection
            options={userOptions}
            value={selectedUser}
            onChange={(_, { value }) => onSelectedUserChange(value || null)}
            clearable
          />
          {showClearButtons && (
            <Button onClick={() => onSelectedUserChange(null)}>Clear</Button>
          )}
        </div>
      )}

      {filterType === "division" && (
        <div style={{ display: "flex", marginTop: gap, gap }}>
          <Dropdown
            placeholder="Select division"
            search
            selection
            options={divisionOptions}
            value={selectedDivision}
            onChange={(_, { value }) => onSelectedDivisionChange(value || null)}
            clearable
          />
          {showClearButtons && (
            <Button onClick={() => onSelectedDivisionChange(null)}>
              Clear
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
