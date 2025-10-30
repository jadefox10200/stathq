import { useState } from "react";
import { Modal, Button } from "semantic-ui-react";

export default function AlertModal() {
  const [open, setOpen] = useState(false);
  const [header, setHeader] = useState("");
  const [message, setMessage] = useState("");
  const [icon, setIcon] = useState("");

  // Expose a global function for jQuery scripts
  window.showAlertModal = (header, message, icon) => {
    setHeader(header);
    setMessage(message);
    setIcon(icon);
    setOpen(true);
  };

  return (
    <Modal size="small" open={open} onClose={() => setOpen(false)}>
      <Modal.Header>{header}</Modal.Header>
      <Modal.Content image>
        <div className={`image ${icon}`}></div>
        <Modal.Description>
          <span style={{ whiteSpace: "pre-line" }}>
            <div>{message}</div>
          </span>
        </Modal.Description>
      </Modal.Content>
      <Modal.Actions>
        <Button color="green" onClick={() => setOpen(false)}>
          OK
        </Button>
      </Modal.Actions>
    </Modal>
  );
}
