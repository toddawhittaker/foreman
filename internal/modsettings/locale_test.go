package modsettings

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

func TestParseLocaleCfg(t *testing.T) {
	in := "\xef\xbb\xbf# a comment\r\n; another\r\n\r\n[mod-setting-name]\r\nfm-sta-int=Start count\r\n  fm-sta-bool = Spaced value  \r\n" +
		"[other-section]\r\nfm-sta-int=ignored\r\n[mod-setting-description]\r\nfm-sta-int=How many; # not a comment\r\n" +
		"bad line without equals\r\n=empty key\r\nfm-eq=a=b=c\r\n[mod-setting-name]\r\nfm-later=Later section\r\n" +
		"before=x\n"
	secs, err := ParseLocaleCfg([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	names, descs := secs["mod-setting-name"], secs["mod-setting-description"]
	if secs["other-section"]["fm-sta-int"] != "ignored" {
		t.Errorf("other section: %q", secs["other-section"])
	}
	wantNames := map[string]string{"fm-sta-int": "Start count", "fm-sta-bool": " Spaced value  ", "fm-later": "Later section", "before": "x"}
	if len(names) != len(wantNames) {
		t.Errorf("names: %q", names)
	}
	for k, v := range wantNames {
		if names[k] != v {
			t.Errorf("names[%q] = %q; want %q", k, names[k], v)
		}
	}
	wantDescs := map[string]string{"fm-sta-int": "How many; # not a comment", "fm-eq": "a=b=c"}
	if len(descs) != len(wantDescs) {
		t.Errorf("descs: %q", descs)
	}
	for k, v := range wantDescs {
		if descs[k] != v {
			t.Errorf("descs[%q] = %q; want %q", k, descs[k], v)
		}
	}
}

func TestParseLocaleCfgValuesAsWritten(t *testing.T) {
	secs, err := ParseLocaleCfg([]byte("[mod-setting-name]\nx=[color=red]Hi[/color] __1__ \\n<b>\n"))
	names := secs["mod-setting-name"]
	if err != nil || names["x"] != `[color=red]Hi[/color] __1__ \n<b>` {
		t.Fatalf("%q %v", names["x"], err)
	}
}

func TestParseLocaleCfgEmptyAndInvalid(t *testing.T) {
	secs, err := ParseLocaleCfg(nil)
	if err != nil || secs == nil || len(secs) != 0 {
		t.Errorf("empty: %v %v", secs, err)
	}
	secs, err = ParseLocaleCfg([]byte("before=x\n[mod-setting-name]\nk\xff=v\xfe\n[s\xff]\na=b\n"))
	if err != nil || secs["mod-setting-name"]["k\uFFFD"] != "v\uFFFD" || secs["s\uFFFD"]["a"] != "b" || len(secs) != 2 {
		t.Errorf("invalid UTF-8: %q %v", secs, err)
	}
}

func TestParseLocaleCfgLimit(t *testing.T) {
	ok := "[mod-setting-name]\na=" + strings.Repeat("x", MaxCfgFileBytes-len("[mod-setting-name]\na="))
	if len(ok) != MaxCfgFileBytes {
		t.Fatalf("test data is %d bytes", len(ok))
	}
	if _, err := ParseLocaleCfg([]byte(ok)); err != nil {
		t.Errorf("file at the limit: %v", err)
	}
	if _, err := ParseLocaleCfg([]byte(ok + "x")); err == nil {
		t.Error("file over the limit accepted")
	}
}

func TestFactorioLocales(t *testing.T) {
	want := map[string]string{"en": "en", "de": "de", "ru": "ru", "zh-Hans": "zh-CN", "fr": "fr", "es": "es-ES", "pl": "pl", "cs": "cs"}
	if !maps.Equal(FactorioLocales, want) {
		t.Errorf("got %v", FactorioLocales)
	}
}

func FuzzParseLocaleCfg(f *testing.F) {
	f.Add([]byte("[mod-setting-name]\na=b\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseLocaleCfg(data)
	})
}

func TestLocaleSectionsMerge(t *testing.T) {
	s := LocaleSections{"a": {"x": "1", "y": "2"}}
	s.Merge(LocaleSections{"a": {"y": "3"}, "b": {"z": "4"}})
	want := LocaleSections{"a": {"x": "1", "y": "3"}, "b": {"z": "4"}}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %v", s)
	}
}
