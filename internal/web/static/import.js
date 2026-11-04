// Fills the folder form's version and Space Age from the chosen folder,
// leaving alone anything the admin has typed or ticked.
document.addEventListener("DOMContentLoaded", () => {
  const version = document.getElementById("folder-version");
  const spaceAge = document.getElementById("folder-space-age");
  if (!version || !spaceAge) return;
  let edited = false;
  version.addEventListener("input", () => { edited = true; });
  spaceAge.addEventListener("change", () => { edited = true; });
  document.querySelectorAll('input[name="folder"]').forEach((radio) => {
    radio.addEventListener("change", () => {
      if (edited || !radio.dataset.gameVersion) return;
      version.value = radio.dataset.gameVersion;
      spaceAge.checked = "spaceAge" in radio.dataset;
    });
  });
});
