import React from "react";
import { Button, Icon } from "semantic-ui-react";
/*
ChartExport - export/print helper for charts rendered as SVG

Props:
- chartRef: React ref pointing at the DOM node that contains the chart's <svg>.
- filename: optional filename base, default "chart"
- title: optional string to render as bold title at top of printed page

Usage:
  <ChartExport chartRef={chartRef} filename="mychart" title="STAT-01 — Sales" />
*/

function escapeHtml(unsafe) {
  if (!unsafe && unsafe !== 0) return "";
  return String(unsafe)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function serializeSvg(svg) {
  const serializer = new XMLSerializer();
  let svgString = serializer.serializeToString(svg);
  if (!svgString.match(/^<\?xml/)) {
    svgString = '<?xml version="1.0" standalone="no"?>\n' + svgString;
  }
  return svgString;
}

async function svgToPngDataUrl(svgElement, scale = 2) {
  const bbox = svgElement.getBBox();
  const width = Math.ceil(bbox.width || svgElement.clientWidth || 800);
  const height = Math.ceil(bbox.height || svgElement.clientHeight || 600);

  const svgString = serializeSvg(svgElement);
  const blob = new Blob([svgString], { type: "image/svg+xml;charset=utf-8" });
  const url = URL.createObjectURL(blob);

  const img = new Image();
  img.crossOrigin = "anonymous";

  await new Promise((resolve, reject) => {
    img.onload = resolve;
    img.onerror = reject;
    img.src = url;
  });

  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(width * scale));
  canvas.height = Math.max(1, Math.round(height * scale));
  const ctx = canvas.getContext("2d");
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
  URL.revokeObjectURL(url);
  return canvas.toDataURL("image/png");
}

function downloadDataUrl(dataUrl, filename) {
  const a = document.createElement("a");
  a.href = dataUrl;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export default function ChartExport({
  chartRef,
  filename = "chart",
  title = "",
  subTitle = "",
}) {
  if (!chartRef) return null;

  const onDownloadSVG = () => {
    const node = chartRef.current;
    if (!node) return alert("Chart not found");
    const svg = node.querySelector("svg");
    if (!svg) return alert("SVG element not found inside chart container");

    const svgString = serializeSvg(svg);
    const blob = new Blob([svgString], { type: "image/svg+xml;charset=utf-8" });
    downloadBlob(blob, `${filename}.svg`);
  };

  const onDownloadPNG = async () => {
    const node = chartRef.current;
    if (!node) return alert("Chart not found");
    const svg = node.querySelector("svg");
    if (!svg) return alert("SVG element not found inside chart container");

    try {
      const dataUrl = await svgToPngDataUrl(svg, 2);
      downloadDataUrl(dataUrl, `${filename}.png`);
    } catch (err) {
      console.error(err);
      alert(
        "Failed to export PNG: " + (err && err.message ? err.message : err)
      );
    }
  };

  const onPrint = () => {
    const node = chartRef.current;
    if (!node) return alert("Chart not found");
    const svg = node.querySelector("svg");
    if (!svg) return alert("SVG element not found inside chart container");

    // Ensure viewBox exists for scaling
    if (!svg.getAttribute("viewBox")) {
      const w = svg.getAttribute("width") || svg.clientWidth || 800;
      const h = svg.getAttribute("height") || svg.clientHeight || 600;
      try {
        svg.setAttribute("viewBox", `0 0 ${w} ${h}`);
      } catch (e) {
        // ignore
      }
    }

    const svgString = serializeSvg(svg);
    const safeTitle = escapeHtml(title);
    const safeSubTitle = escapeHtml(subTitle);

    const html = `<!doctype html>
      <html>
        <head>
          <meta charset="utf-8"/>
          <title>Print chart</title>
          <style>
            /* Page & body */
            @page { size: auto; margin: 12mm; }
            html,body { height: 100%; margin: 0; padding: 0; background: #fff; }
            /* Centering container */
            .page { box-sizing: border-box; min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 16px; }
            .inner { width: 100%; max-width: 1100px; }
            /* Title */
            .title { font-family: Arial, Helvetica, sans-serif; font-size: 18px; font-weight: 700; text-align: center; margin-bottom: 12px; }
            .subTitle { font-family: Arial, Helvetica, sans-serif; font-size: 14px; font-weight: 400; text-align: center; margin-bottom: 24px; color: #333; }
            /* Ensure the svg scales to container width, preserving aspect */
            .chart-wrap svg { width: 100% !important; height: auto !important; display: block; margin: 0 auto; }
            @media print {
              body { background: #fff !important; }
            }
          </style>
        </head>
        <body>
          <div class="page">
            <div class="inner">
              ${safeTitle ? `<div class="title">${safeTitle}</div>` : ""}
              ${
                safeSubTitle
                  ? `<div class="subTitle">${safeSubTitle}</div>`
                  : ""
              }
              
              <div class="chart-wrap">
                ${svgString}
              </div>
            </div>
          </div>
          <script>
            (function() {
              function doPrint() {
                try {
                  window.focus();
                  setTimeout(function(){ window.print(); }, 300);
                } catch(e) {
                  console.error(e);
                  window.print();
                }
              }
              if (document.fonts && document.fonts.ready) {
                document.fonts.ready.then(doPrint).catch(doPrint);
              } else {
                window.onload = doPrint;
              }
            })();
          </script>
        </body>
      </html>`;

    const newWin = window.open("", "_blank");
    if (!newWin) {
      alert(
        "Unable to open print window (popup blocked). Please allow popups and try again."
      );
      return;
    }
    newWin.document.open();
    newWin.document.write(html);
    newWin.document.close();
  };

  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
      <Button size="tiny" onClick={onDownloadSVG}>
        <Icon name="file image outline" /> Download SVG
      </Button>
      <Button size="tiny" onClick={onDownloadPNG}>
        <Icon name="download" /> Download PNG
      </Button>
      <Button size="tiny" onClick={onPrint}>
        <Icon name="print" /> Print
      </Button>
    </div>
  );
}
