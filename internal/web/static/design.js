// The component page's one script: a mixed checkbox has no attribute, only a
// property, so the specimens that show one are marked and set here.
for (const box of document.querySelectorAll("input[data-indeterminate]")) box.indeterminate = true;
