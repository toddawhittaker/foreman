local defs = {}
for _, st in ipairs({"startup","runtime-global","runtime-per-user"}) do
  local p = ({startup="fm-sta-",["runtime-global"]="fm-glo-",["runtime-per-user"]="fm-usr-"})[st]
  table.insert(defs, {type="bool-setting", name=p.."bool", setting_type=st, default_value=true, order="a"})
  table.insert(defs, {type="int-setting", name=p.."int", setting_type=st, default_value=5, minimum_value=1, maximum_value=10, order="b"})
  table.insert(defs, {type="double-setting", name=p.."double", setting_type=st, default_value=1.5, minimum_value=0.5, maximum_value=9.5, order="c"})
  table.insert(defs, {type="string-setting", name=p.."string", setting_type=st, default_value="red", allowed_values={"red","green","blue"}, order="d"})
  table.insert(defs, {type="color-setting", name=p.."color", setting_type=st, default_value={r=1,g=0.5,b=0,a=1}, order="e"})
end
table.insert(defs, {type="bool-setting", name="fm-sta-hidden", setting_type="startup", default_value=false, hidden=true})
table.insert(defs, {type="int-setting", name="fm-sta-forced", setting_type="startup", default_value=3, allowed_values={3}, forced_value=3})
table.insert(defs, {type="string-setting", name="fm-sta-blank", setting_type="startup", default_value="hello", allow_blank=true, auto_trim=true, order="f"})
table.insert(defs, {type="string-setting", name="fm-sta-long", setting_type="startup", default_value=string.rep("0123456789", 30), order="g"})
table.insert(defs, {type="int-setting", name="fm-sta-unbounded", setting_type="startup", default_value=4, order="h"})
table.insert(defs, {type="double-setting", name="fm-sta-dallowed", setting_type="startup", default_value=1, allowed_values={0.5, 1, 2}, order="i"})
data:extend(defs)
