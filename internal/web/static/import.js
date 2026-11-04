// Suggests each form's world name from the chosen folder or file, and fills
// the folder form's version and Space Age from the chosen folder, leaving
// alone anything the admin has typed or ticked. A folder whose save could
// not be read clears what this script filled in before.
document.addEventListener("DOMContentLoaded", () => {
  // Reads the change out politely, emptying the region first so a repeated
  // sentence is still read.
  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  // Keeps a name field filled with suggestions until the admin types in it.
  // suggested is what this script, or the server, last put there.
  function nameSuggester(field, suggested) {
    if (!field) return () => {};
    let edited = field.value !== "" && field.value !== suggested;
    field.addEventListener("input", () => { edited = true; });
    return (name) => {
      if (edited) return;
      field.value = name;
    };
  }

  const checked = document.querySelector('input[name="folder"]:checked');
  const suggestFolderName = nameSuggester(document.getElementById("folder-name"), checked ? checked.value : "");
  const suggestUploadName = nameSuggester(document.getElementById("upload-name"), "");

  const file = document.getElementById("upload-source");
  if (file) {
    file.addEventListener("change", () => {
      if (file.files.length === 1) suggestUploadName(file.files[0].name.replace(/\.zip$/i, ""));
    });
  }

  const version = document.getElementById("folder-version");
  const spaceAge = document.getElementById("folder-space-age");
  let edited = false;
  // filled is set while the fields hold values this script put there.
  // A form the server sent back after a failed submit may hold the chosen
  // folder's values, filled from its save: those count as filled, not typed.
  let filled = !!checked && !!checked.dataset.gameVersion && !!version && !!spaceAge &&
    version.value === checked.dataset.gameVersion &&
    spaceAge.checked === ("spaceAge" in checked.dataset);
  if (version) version.addEventListener("input", () => { edited = true; });
  if (spaceAge) spaceAge.addEventListener("change", () => { edited = true; });

  document.querySelectorAll('input[name="folder"]').forEach((radio) => {
    radio.addEventListener("change", () => {
      suggestFolderName(radio.value);
      if (edited || !version || !spaceAge) return;
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
