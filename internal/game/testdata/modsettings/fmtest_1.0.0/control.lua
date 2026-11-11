local function dump()
  for _, n in ipairs({"fm-sta-bool","fm-sta-int","fm-sta-double","fm-sta-string","fm-sta-color"}) do
    log("FMSET startup " .. n .. "=" .. serpent.line(settings.startup[n].value))
  end
  for _, n in ipairs({"fm-glo-int","fm-glo-string"}) do
    log("FMSET global " .. n .. "=" .. serpent.line(settings.global[n].value))
  end
end
script.on_init(dump)
script.on_load(function() log("FMSET load startup fm-sta-int=" .. settings.startup["fm-sta-int"].value .. " global fm-glo-int=" .. settings.global["fm-glo-int"].value) end)
