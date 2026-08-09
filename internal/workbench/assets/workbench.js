document.addEventListener("dragstart", (event) => {
  const handoff = event.target.closest("[data-artifact-handoff]");
  if (!handoff || !event.dataTransfer) return;

  const endpoint = new URL("/project/artifact", window.location.href).href;
  const name = handoff.dataset.artifactName || "volume.atlas";
  event.dataTransfer.effectAllowed = "copy";
  event.dataTransfer.setData("DownloadURL", `application/octet-stream:${name}:${endpoint}`);
  event.dataTransfer.setData("text/uri-list", endpoint);
  event.dataTransfer.setData("text/plain", handoff.dataset.artifactPath || name);
});

const tileX = (longitude, count, upper = false) => {
  const value = ((longitude + 180) / 360) * count;
  return upper ? Math.ceil(value) - 1 : Math.floor(value);
};

const tileY = (latitude, count) => {
  const radians = latitude * Math.PI / 180;
  return Math.floor((1 - Math.asinh(Math.tan(radians)) / Math.PI) / 2 * count);
};

const latitudeAtTile = (tile, count) => Math.atan(Math.sinh(Math.PI * (1 - 2 * tile / count))) * 180 / Math.PI;

const mapRegions = {
  world: {
    extent: [-180, -85.051128, 180, 85.051128], initial: [-105.1, 39.6, -104.9, 39.8], zoom: 12,
    outline: [],
  },
  conus: {
    extent: [-125, 24, -66, 50], initial: [-105.1, 39.6, -104.9, 39.8], zoom: 12,
    outline: [[-124, 48], [-124, 42], [-122, 38], [-117, 33], [-111, 32], [-106, 31], [-103, 29], [-97, 26], [-82, 25], [-80, 27], [-81, 31], [-75, 35], [-67, 45], [-75, 45], [-83, 46], [-95, 49], [-112, 49]],
  },
  alaska: {
    extent: [-180, 50, -129, 72], initial: [-151.0, 60.9, -150.7, 61.2], zoom: 11,
    outline: [[-168, 54], [-160, 55], [-153, 57], [-147, 59], [-141, 60], [-141, 69], [-151, 71], [-162, 68], [-168, 64], [-179, 52]],
  },
  hawaii: {
    extent: [-161, 18, -154, 23], initial: [-157.95, 21.25, -157.7, 21.45], zoom: 12,
    outline: [[-160.3, 22.1], [-159.2, 21.8], [-158.2, 21.7], [-157.7, 21.3], [-156.3, 20.8], [-155.5, 19.4]],
  },
  "puerto-rico": {
    extent: [-68, 17, -65, 19], initial: [-66.7, 18.1, -66.3, 18.4], zoom: 13,
    outline: [[-67.3, 18.5], [-65.6, 18.5], [-65.2, 18.2], [-66.0, 17.9], [-67.2, 18.0]],
  },
};

const alignedSelection = (form) => {
  const value = (name) => Number(form.elements.namedItem(name).value);
  const zoom = value("detail");
  const count = 2 ** zoom;
  const minX = tileX(value("west"), count);
  const maxX = tileX(value("east"), count, true);
  const minY = tileY(value("north"), count);
  const maxY = tileY(value("south"), count);
  let side = 1;
  while (Math.floor(minX / side) * side + side - 1 < maxX || Math.floor(minY / side) * side + side - 1 < maxY) side *= 2;
  const originX = Math.floor(minX / side) * side;
  const originY = Math.floor(minY / side) * side;
  return {
    zoom, side, pixels: side * 256, localZoom: Math.log2(side),
    bounds: [originX / count * 360 - 180, latitudeAtTile(originY + side, count),
      (originX + side) / count * 360 - 180, latitudeAtTile(originY, count)],
  };
};

const mapPoint = (bounds, canvas, longitude, latitude) => [
  (longitude - bounds[0]) / (bounds[2] - bounds[0]) * canvas.width,
  (bounds[3] - latitude) / (bounds[3] - bounds[1]) * canvas.height,
];

const drawBounds = (context, canvas, region, bounds, color, fill) => {
  const start = mapPoint(region.extent, canvas, bounds[0], bounds[3]);
  const end = mapPoint(region.extent, canvas, bounds[2], bounds[1]);
  context.strokeStyle = color;
  context.fillStyle = fill;
  context.lineWidth = 3;
  context.setLineDash(color === "#7fc5de" ? [] : [10, 6]);
  context.fillRect(start[0], start[1], end[0] - start[0], end[1] - start[1]);
  context.strokeRect(start[0], start[1], end[0] - start[0], end[1] - start[1]);
};

const renderAreaMap = (form, aligned) => {
  const canvas = form.querySelector("[data-area-map]");
  const region = mapRegions[form.querySelector("[data-map-region]").value];
  const context = canvas.getContext("2d");
  context.clearRect(0, 0, canvas.width, canvas.height);
  context.fillStyle = "#0d1626";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.beginPath();
  region.outline.forEach(([longitude, latitude], index) => {
    const point = mapPoint(region.extent, canvas, longitude, latitude);
    if (index === 0) context.moveTo(point[0], point[1]); else context.lineTo(point[0], point[1]);
  });
  if (region.outline.length > 4) context.closePath();
  context.fillStyle = "rgba(138, 106, 68, .35)";
  context.strokeStyle = "#8a6a44";
  context.lineWidth = 2;
  context.fill();
  context.stroke();
  const value = (name) => Number(form.elements.namedItem(name).value);
  drawBounds(context, canvas, region, aligned.bounds, "#8e897c", "rgba(0,0,0,0)");
  drawBounds(context, canvas, region, [value("west"), value("south"), value("east"), value("north")], "#7fc5de", "rgba(32, 138, 174, .22)");
};

const updateAreaBudget = (form) => {
  const budget = form.querySelector("[data-budget-preview]");
  const detail = form.querySelector("[data-detail-output]");
  const selected = alignedSelection(form);
  const rasterTiles = (4 ** (selected.localZoom + 1) - 1) / 3;
  const captures = (form.elements.namedItem("topo").checked ? 1 : 0) +
    (form.elements.namedItem("roads").checked ? 3 : 0) +
    (form.elements.namedItem("hydro").checked ? 2 : 0) +
    (form.elements.namedItem("counties").checked ? 1 : 0);
  detail.value = String(selected.zoom);
  budget.querySelector("[data-budget-tiles]").value = rasterTiles.toLocaleString();
  budget.querySelector("[data-budget-size]").value = `${selected.pixels.toLocaleString()} × ${selected.pixels.toLocaleString()}`;
  const over = selected.pixels > 4096 || captures === 0;
  budget.classList.toggle("over", over);
  budget.querySelector("[data-budget-state]").value = captures === 0 ? "Select at least one source." :
    over ? `${captures} captures · lower detail or narrow the area.` : `${captures} capture requests · within default gate.`;
  renderAreaMap(form, selected);
};

const installAreaMap = (form) => {
  const canvas = form.querySelector("[data-area-map]");
  const regionPicker = form.querySelector("[data-map-region]");
  let start = null;
  const coordinates = (event) => {
    const rect = canvas.getBoundingClientRect();
    const x = Math.max(0, Math.min(canvas.width, (event.clientX - rect.left) / rect.width * canvas.width));
    const y = Math.max(0, Math.min(canvas.height, (event.clientY - rect.top) / rect.height * canvas.height));
    const region = mapRegions[regionPicker.value];
    return [region.extent[0] + x / canvas.width * (region.extent[2] - region.extent[0]),
      region.extent[3] - y / canvas.height * (region.extent[3] - region.extent[1])];
  };
  canvas.addEventListener("pointerdown", (event) => {
    start = coordinates(event);
    canvas.setPointerCapture(event.pointerId);
  });
  canvas.addEventListener("pointermove", (event) => {
    if (!start) return;
    const end = coordinates(event);
    form.elements.namedItem("west").value = Math.min(start[0], end[0]).toFixed(6);
    form.elements.namedItem("east").value = Math.max(start[0], end[0]).toFixed(6);
    form.elements.namedItem("south").value = Math.min(start[1], end[1]).toFixed(6);
    form.elements.namedItem("north").value = Math.max(start[1], end[1]).toFixed(6);
    updateAreaBudget(form);
  });
  canvas.addEventListener("pointerup", (event) => {
    if (start) canvas.releasePointerCapture(event.pointerId);
    start = null;
  });
  regionPicker.addEventListener("change", () => {
    const region = mapRegions[regionPicker.value];
    ["west", "south", "east", "north"].forEach((name, index) => { form.elements.namedItem(name).value = region.initial[index]; });
    form.elements.namedItem("detail").value = region.zoom;
    updateAreaBudget(form);
  });
};

document.querySelectorAll("[data-us-area-form]").forEach((form) => {
  installAreaMap(form);
  updateAreaBudget(form);
  form.addEventListener("input", () => updateAreaBudget(form));
});
