package modsettings

// Limits on untrusted input (SPEC 14.6).
const (
	// MaxDefinitionsBytes caps the definitions file a run writes.
	MaxDefinitionsBytes = 2 << 20
	// MaxSettings caps the number of settings in one definitions file.
	MaxSettings = 10000
	// MaxAllowedValues caps the allowed values of one setting.
	MaxAllowedValues = 1000
	// MaxStringBytes caps an edited string value.
	MaxStringBytes = 10000
	// MaxCfgFileBytes caps one locale .cfg file.
	MaxCfgFileBytes = 1 << 20
	// MaxCfgLanguageBytes caps all .cfg files read for one language.
	MaxCfgLanguageBytes = 8 << 20
)

// SettingType is where a setting lives in mod-settings.dat.
type SettingType string

// The three setting types, spelled as Factorio spells them.
const (
	Startup        SettingType = "startup"
	RuntimeGlobal  SettingType = "runtime-global"
	RuntimePerUser SettingType = "runtime-per-user"
)

// ValueType is the kind of value a setting holds.
type ValueType string

// The value types Foreman can edit. Any other type is shown as not editable.
const (
	TypeBool   ValueType = "bool-setting"
	TypeInt    ValueType = "int-setting"
	TypeDouble ValueType = "double-setting"
	TypeString ValueType = "string-setting"
	TypeColor  ValueType = "color-setting"
)

// Color is a color setting's value, each component 0 to 1.
type Color struct {
	R, G, B, A float64
}

// Value is one typed setting value. Only the field for Type is meaningful.
type Value struct {
	Type   ValueType
	Bool   bool
	Int    int64
	Double float64
	String string
	Color  Color
}

// Definition is one mod setting as the game's prototypes describe it.
// Default, Min and Max are nil when the prototype does not set them.
type Definition struct {
	Name          string
	Mod           string
	Setting       SettingType
	Type          ValueType
	Default       *Value
	Min           *Value
	Max           *Value
	AllowedValues []Value
	Hidden        bool
	AllowBlank    bool
	AutoTrim      bool
	Order         string
	// Editable is false for an unknown type or a name Foreman will not touch.
	Editable bool
}

// Definitions is the parsed list from one definitions run, in file order.
type Definitions []Definition

// ValidationError is a refused value: a message id for the page and its
// parameters (the setting's shown name and the limit). It never holds the
// raw value.
type ValidationError struct {
	MessageID string
	Params    map[string]string
}

// Error returns the message id, for logs.
func (e *ValidationError) Error() string { return e.MessageID }

// LocaleText is one setting's shown name and description in one language.
type LocaleText struct {
	Name        string
	Description string
}

// Locale holds setting text keyed by Foreman language code, then by setting
// name.
type Locale map[string]map[string]LocaleText

// FactorioLocales maps each of Foreman's eight language codes to the folder
// name Factorio uses under a mod's locale/.
var FactorioLocales = map[string]string{
	"en":      "en",
	"de":      "de",
	"ru":      "ru",
	"zh-Hans": "zh-CN",
	"fr":      "fr",
	"es":      "es-ES",
	"pl":      "pl",
	"cs":      "cs",
}
