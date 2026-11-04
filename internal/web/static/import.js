// Fills the folder form's version and Space Age from the chosen folder,
// leaving alone anything the admin has typed or ticked. A folder whose save
// could not be read clears what this script filled in before.
document.addEventListener("DOMContentLoaded", () => {
  const version = document.getElementById("folder-version");
  const spaceAge = document.getElementById("folder-space-age");
  if (!version || !spaceAge) return;
  let edited = false;
  // filled is set while the fields hold values this script put there.
  let filled = false;
  version.addEventListener("input", () => { edited = true; });
  spaceAge.addEventListener("change", () => { edited = true; });

  // Reads the change out politely, emptying the region first so a repeated
  // sentence is still read.
  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  document.querySelectorAll('input[name="folder"]').forEach((radio) => {
    radio.addEventListener("change", () => {
      if (edited) return;
      if (radio.dataset.gameVersion) {
        version.value = radio.dataset.gameVersion;
        spaceAge.checked = "spaceAge" in radio.dataset;
        filled = true;
        announce("Game version set to " + version.value + " from the save" +
          (spaceAge.checked ? ", with Space Age on." : "."));
      } else if (filled) {
        version.value = "";
        spaceAge.checked = false;
        filled = false;
        announce("Foreman could not read this folder's save, so the game version is empty. Enter it.");
      }
    });
  });
});
