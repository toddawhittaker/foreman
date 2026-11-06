/// <reference types="node" />
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// Tests choose text by catalogue key, not by wording, so a wording change in
// a catalogue never breaks a test (SPEC 22). The catalogue is read from the
// same files Foreman embeds.

export const languages = ["en", "de", "ru", "zh-Hans", "fr", "es", "pl", "cs"] as const;
export type Lang = (typeof languages)[number];

export type Entry = string | Record<string, string>;

const localesDir = join(import.meta.dirname, "..", "..", "internal", "i18n", "locales");
const loaded = new Map<Lang, Record<string, Entry>>();

export function catalogue(lang: Lang): Record<string, Entry> {
  let cat = loaded.get(lang);
  if (!cat) {
    cat = {};
    const dir = join(localesDir, lang);
    for (const file of readdirSync(dir).filter((f) => f.endsWith(".json"))) {
      Object.assign(cat, JSON.parse(readFileSync(join(dir, file), "utf8")));
    }
    loaded.set(lang, cat);
  }
  return cat;
}

// The plural category of a whole number in a language, from the platform's
// copy of CLDR.
export function pluralCategory(lang: Lang, n: number): string {
  return new Intl.PluralRules(lang).select(n);
}

// The text of a key in a language with its placeholders filled. A key with
// plural forms needs a numeric `count`.
export function t(key: string, args: Record<string, string | number> = {}, lang: Lang = "en"): string {
  const entry = catalogue(lang)[key];
  if (entry === undefined) throw new Error(`no key ${key} in ${lang}`);
  let text: string;
  if (typeof entry === "string") {
    text = entry;
  } else {
    if (typeof args.count !== "number") throw new Error(`${key} has plural forms and needs a numeric count`);
    const cat = pluralCategory(lang, args.count);
    if (entry[cat] === undefined) throw new Error(`${key} in ${lang} has no ${cat} form for ${args.count}`);
    text = entry[cat];
  }
  return text.replace(/\{(\w+)\}/g, (_, name: string) => {
    if (!(name in args)) throw new Error(`${key}: no value for {${name}}`);
    return String(args[name]);
  });
}
