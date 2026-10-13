package whispercli

// LanguageAuto lets whisper-cli detect the spoken language.
const LanguageAuto = "auto"

// languages is the allowlist of -l values: "auto" and the codes of g_lang in src/whisper.cpp v1.9.5.
var languages = map[string]bool{
	LanguageAuto: true,
	"en":         true, "zh": true, "de": true, "es": true, "ru": true, "ko": true, "fr": true, "ja": true, "pt": true,
	"tr": true, "pl": true, "ca": true, "nl": true, "ar": true, "sv": true, "it": true, "id": true, "hi": true,
	"fi": true, "vi": true, "he": true, "uk": true, "el": true, "ms": true, "cs": true, "ro": true, "da": true,
	"hu": true, "ta": true, "no": true, "th": true, "ur": true, "hr": true, "bg": true, "lt": true, "la": true,
	"mi": true, "ml": true, "cy": true, "sk": true, "te": true, "fa": true, "lv": true, "bn": true, "sr": true,
	"az": true, "sl": true, "kn": true, "et": true, "mk": true, "br": true, "eu": true, "is": true, "hy": true,
	"ne": true, "mn": true, "bs": true, "kk": true, "sq": true, "sw": true, "gl": true, "mr": true, "pa": true,
	"si": true, "km": true, "sn": true, "yo": true, "so": true, "af": true, "oc": true, "ka": true, "be": true,
	"tg": true, "sd": true, "gu": true, "am": true, "yi": true, "lo": true, "uz": true, "fo": true, "ht": true,
	"ps": true, "tk": true, "nn": true, "mt": true, "sa": true, "lb": true, "my": true, "bo": true, "tl": true,
	"mg": true, "as": true, "tt": true, "haw": true, "ln": true, "ha": true, "ba": true, "jw": true, "su": true,
	"yue": true,
}
