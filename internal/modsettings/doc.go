// Package modsettings reads and writes Factorio's mod-settings.dat, parses
// the setting definitions a control-mod run writes, checks edited values
// against those definitions, and reads setting names and descriptions from
// mods' locale .cfg files (SPEC 9.6, 14.6).
//
// Every input here is untrusted: the definitions file and .cfg files come
// from mods, and mod-settings.dat may be uploaded. Parsers enforce the size
// and count limits and keep the bytes of anything they do not know.
package modsettings
