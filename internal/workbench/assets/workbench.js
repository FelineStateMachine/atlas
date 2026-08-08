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
