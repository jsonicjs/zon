/* Copyright (c) 2025 Richard Rodger, MIT License */

// Package zon is a jsonic plugin that parses Zig Object Notation (ZON)
// syntax. ZON is a data format based on Zig anonymous struct literals.
//
// Example:
//
//	.{
//	    .name = "example",
//	    .version = "0.0.1",
//	    .deps = .{ .foo = .{ .url = "https://..." } },
//	    .paths = .{ "build.zig", "src" },
//	}
package zon

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	jsonic "github.com/jsonicjs/jsonic/go"
)

const Version = "0.1.0"

// --- BEGIN EMBEDDED zon-grammar.jsonic ---
const grammarText = `
# ZON Grammar Definition
# Parses Zig Object Notation (ZON) - a data format based on Zig anonymous
# struct literals.
#
# Example:
#   .{
#       .name = "example",
#       .version = "0.0.1",
#       .deps = .{ .foo = .{ .url = "https://..." } },
#       .paths = .{ "build.zig", "src" },
#   }
#
# Lex strategy:
#   . is a fixed token (#DT) and {, } are the usual #OB / #CB.
#   The default text lexer produces #TX tokens for bare identifiers (which
#   are only ever legal after a . in ZON). The grammar below reassembles
#   these tokens into struct, tuple, pair and enum-literal constructs using
#   multi-token alt lookahead — so the plugin needs no custom matcher for
#   .{ or .identifier.
#
# Only two things the lexer still does are Zig-specific enough to warrant
# their own matchers (both declared in the plugin code):
#   - zonMultiString: consecutive \\-prefixed lines.
#   - zonChar:        'x', '\n', '\xNN', '\u{...}' literals.
#
# Field-key vs value disambiguation:
#   KEY is narrowed to [#TX] so only identifiers can appear on the left of
#   =. VAL is narrowed to [#NR, #ST, #VL] so a bare identifier cannot
#   stand in as a value — enum literals must be written .name.
#
# The grammar is applied with { rule: { alt: { g: 'zon' } } } so every
# alt below is automatically tagged with the 'zon' group.
#
# Two option overrides still live in plugin code because they can't appear
# in grammar text:
#   - fixed.token   (null values to delete tokens; the Go MapToOptions
#                   does not translate this key)
#   - lex.match     (holds closures over plugin options)

{
  options: {
    rule: {
      # Remove jsonic extensions (implicit maps/lists, top-level commas,
      # path dives). ZON uses explicit struct literals only.
      exclude: 'jsonic,imp'
      start: 'val'
    }
    tokenSet: {
      # Field names are identifiers only (no strings or numbers as keys).
      KEY: ['#TX']
      # Bare identifiers are NOT valid values — force enum literals to be
      # written as .name. VAL omits #TX so a stray foo is a syntax
      # error rather than a silent string.
      VAL: ['#NR' '#ST' '#VL']
    }
    string: {
      chars: '"'
      multiChars: ''
      # Zig-flavoured escape sequences.
      escape: {
        n: '\n'
        r: '\r'
        t: '\t'
        '\\': '\\'
        '"': '"'
        "'": "'"
      }
      allowUnknown: false
    }
    number: {
      lex: true
      sep: '_'
    }
    # Only // line comments in ZON.
    comment: {
      lex: true
      def: {
        hash: { lex: false }
        slash: { line: true start: '//' lex: true eatline: false }
        multi: { lex: false }
      }
    }
    value: {
      lex: true
      def: {
        'true': { val: true }
        'false': { val: false }
        'null': { val: null }
      }
    }
    # The default text lexer produces #TX tokens for bare identifiers.
    text: {
      lex: true
    }
  }

  rule: val: open: {
    alts: [
      # Empty .{}  -> empty list.
      { s: '#DT #OB #CB' b: 3 p: list g: 'list,empty' }
      # Struct literal: .{ .key = ...  (5-token lookahead).
      { s: '#DT #OB #DT #TX #CL' b: 5 p: map g: 'map' }
      # Tuple literal: .{ value, ... }
      { s: '#DT #OB' b: 2 p: list g: 'list' }
      # Enum literal used as a value: .name
      { s: '#DT #TX' a: '@enum-val' g: 'enum' }
    ]
    # Remove the default '#OB p:map' and '#OS p:list' json alts, which
    # would otherwise let a bare { or [ open a struct or list.
    inject: { delete: [-3 -2] }
  }

  rule: map: open: {
    alts: [
      { s: '#DT #OB' p: pair g: 'map,open' }
    ]
    # Remove the default '#OB #CB' (empty map) and '#OB p:pair' json alts.
    inject: { delete: [-2 -1] }
  }
  # map.close stays default: matches #CB.

  rule: list: open: {
    alts: [
      { s: '#DT #OB #CB' b: 1 g: 'list,empty' }
      { s: '#DT #OB' p: elem g: 'list,open' }
    ]
    # Remove the default '#OS #CS' and '#OS p:elem' json alts.
    inject: { delete: [-2 -1] }
  }
  rule: list: close: {
    alts: [
      { s: '#CB' g: 'list,close' }
    ]
    # Default list.close matches '#CS' which we never emit; remove it so
    # the list cleanly closes on '}' (#CB) only.
    inject: { delete: [-1] }
  }

  rule: pair: open: {
    alts: [
      { s: '#DT #TX #CL' p: val a: '@pairkey' u: { pair: true } g: 'pair' }
    ]
    # Remove the default '#KEY #CL' json alt: its @pairkey action reads
    # r.o[0] which is now the '.' token, not the field name.
    inject: { delete: [-1] }
  }
  rule: pair: close: {
    alts: [
      { s: '#CA #CB' b: 1 g: 'pair,trailing' }
      { s: '#CA' r: pair g: 'pair,next' }
      { s: '#CB' b: 1 g: 'pair,end' }
    ]
    # Default pair.close already has '#CB b:1' and '#CA r:pair' — remove
    # those duplicates while preserving the '#ZZ' finish alt.
    inject: { delete: [-3 -2] }
  }

  rule: elem: close: {
    alts: [
      { s: '#CA #CB' b: 1 g: 'elem,trailing' }
      { s: '#CA' r: elem g: 'elem,next' }
      { s: '#CB' b: 1 g: 'elem,end' }
    ]
    # Default elem.close has '#CA r:elem' and '#CS b:1' — remove them
    # (we close on '}' via #CB), keeping the '#ZZ' finish alt.
    inject: { delete: [-3 -2] }
  }
}
`
// --- END EMBEDDED zon-grammar.jsonic ---

// Zon is a jsonic plugin that adds ZON parsing support.
// Options are pre-merged with Defaults by jsonic.UseDefaults.
func Zon(j *jsonic.Jsonic, options map[string]any) error {
	// Guard against re-invocation: SetOptions triggers plugin re-application.
	if j.Decoration("zon-init") != nil {
		return nil
	}
	j.Decorate("zon-init", true)

	charAsNumber := toBool(options["charAsNumber"])
	enumTag := toString(options["enumTag"])

	// Grammar actions:
	//   @pairkey  - capture the identifier at r.O[1] as the pair key
	//               (the 3-token 'DT TX CL' match puts the name in slot 1).
	//   @enum-val - produce the value for a `.name` enum literal appearing
	//               as a value; wrap in map[string]any{enumTag: name} when
	//               configured.
	refs := map[jsonic.FuncRef]any{
		"@pairkey": jsonic.AltAction(func(r *jsonic.Rule, _ *jsonic.Context) {
			if len(r.O) < 2 || r.O[1] == nil {
				return
			}
			tkn := r.O[1]
			if s, ok := tkn.Val.(string); ok {
				r.U["key"] = s
			} else {
				r.U["key"] = tkn.Src
			}
		}),
		"@enum-val": jsonic.AltAction(func(r *jsonic.Rule, _ *jsonic.Context) {
			if len(r.O) < 2 || r.O[1] == nil {
				return
			}
			name, _ := r.O[1].Val.(string)
			if enumTag != "" {
				r.Node = map[string]any{enumTag: name}
			} else {
				r.Node = name
			}
		}),
	}

	gs, err := parseGrammarText(grammarText, refs)
	if err != nil {
		return err
	}
	// All option overrides that can be expressed declaratively live inside
	// the grammar text (see zon-grammar.jsonic) and are translated through
	// gs.OptionsMap. The two that cannot are applied here via gs.Options
	// (jsonic.Grammar applies both):
	//   - fixed.token  registers `.` as #DT and deletes `[`/`]`; not
	//                  translated by MapToOptions.
	//   - lex.match    holds matcher closures that capture plugin options.
	dotSrc := "."
	eqSrc := "="
	gs.Options = &jsonic.Options{
		Fixed: &jsonic.FixedOptions{
			Token: map[string]*string{
				// Register `.` as its own token so the grammar can match
				// `#DT #OB`, `#DT #TX`, etc. via multi-token alt lookahead.
				"#DT": &dotSrc,
				// Bare `[`/`]` are not valid in ZON.
				"#OS": nil,
				"#CS": nil,
				// `=` replaces `:` as the key/value separator.
				"#CL": &eqSrc,
			},
		},
		Lex: &jsonic.LexOptions{
			Match: map[string]*jsonic.MatchSpec{
				"zonMultiString": {Order: 110000, Make: buildZonMultiStringMatcher()},
				"zonChar":        {Order: 120000, Make: buildZonCharMatcher(charAsNumber)},
			},
		},
	}
	// Tag every alt in this grammar with the 'zon' group so callers can
	// selectively exclude zon alts via rule.exclude.
	setting := &jsonic.GrammarSetting{
		Rule: &jsonic.GrammarSettingRule{
			Alt: &jsonic.GrammarSettingAlt{G: "zon"},
		},
	}
	if err := j.Grammar(gs, setting); err != nil {
		return fmt.Errorf("zon: failed to apply grammar: %w", err)
	}

	return nil
}

// Defaults matches the TS Zon.defaults. Used with jsonic.UseDefaults.
var Defaults = map[string]any{
	"charAsNumber": false,
	"enumTag":      "",
}

// ZonOptions is a typed wrapper for common plugin options.
// Fields are pointers so callers can express "omit" (nil) vs "set".
type ZonOptions struct {
	// CharAsNumber, when true, parses Zig char literals ('x') as numeric
	// code points. When false (default), they are parsed as one-char strings.
	CharAsNumber *bool
	// EnumTag, when non-empty, wraps enum literals (.foo used as value) in
	// map[string]any{<EnumTag>: name} instead of producing the bare string.
	EnumTag string
}

func (o ZonOptions) toMap() map[string]any {
	m := map[string]any{}
	if o.CharAsNumber != nil {
		m["charAsNumber"] = *o.CharAsNumber
	}
	if o.EnumTag != "" {
		m["enumTag"] = o.EnumTag
	}
	return m
}

// MakeJsonic returns a reusable Jsonic instance configured for ZON parsing.
// Use this when parsing multiple ZON strings with the same options.
func MakeJsonic(opts ...ZonOptions) *jsonic.Jsonic {
	j := jsonic.Make()
	var m map[string]any
	if len(opts) > 0 {
		m = opts[0].toMap()
	}
	if err := j.UseDefaults(Zon, Defaults, m); err != nil {
		// Plugin registration errors are programming errors with static
		// inputs; surface them via panic rather than silent misbehavior.
		panic(fmt.Sprintf("zon: plugin initialisation failed: %v", err))
	}
	return j
}

// Parse parses a ZON string and returns the resulting value. Convenience
// wrapper around MakeJsonic(opts...).Parse(src).
func Parse(src string, opts ...ZonOptions) (any, error) {
	return MakeJsonic(opts...).Parse(src)
}

// Multi-line Zig strings: consecutive lines starting with `\\`. Each `\\`
// line contributes its content verbatim (after the `\\`); lines join with `\n`.
func buildZonMultiStringMatcher() jsonic.MakeLexMatcher {
	return func(cfg *jsonic.LexConfig, _ *jsonic.Options) jsonic.LexMatcher {
		return func(lex *jsonic.Lex, _ *jsonic.Rule) *jsonic.Token {
			pnt := lex.Cursor()
			src := lex.Src
			if pnt.SI+1 >= len(src) || src[pnt.SI] != '\\' || src[pnt.SI+1] != '\\' {
				return nil
			}

			startI := pnt.SI
			startCI := pnt.CI
			sI := pnt.SI
			rI := pnt.RI
			var parts []string

			for sI+1 < len(src) && src[sI] == '\\' && src[sI+1] == '\\' {
				sI += 2
				lineStart := sI
				for sI < len(src) && !cfg.LineChars[rune(src[sI])] {
					sI++
				}
				parts = append(parts, src[lineStart:sI])

				// Consume line terminator (handle \r\n as one).
				if sI < len(src) && cfg.LineChars[rune(src[sI])] {
					ch := src[sI]
					if cfg.RowChars[rune(ch)] {
						rI++
					}
					sI++
					if sI < len(src) && ch == '\r' && src[sI] == '\n' {
						sI++
					}
				}

				// Look for another `\\` continuation after whitespace.
				peek := sI
				for peek < len(src) && (src[peek] == ' ' || src[peek] == '\t') {
					peek++
				}
				if peek+1 >= len(src) || src[peek] != '\\' || src[peek+1] != '\\' {
					break
				}
				sI = peek
			}

			val := strings.Join(parts, "\n")
			tsrc := src[startI:sI]
			tkn := lex.Token("#ST", jsonic.TinST, val, tsrc)
			pnt.SI = sI
			pnt.RI = rI
			pnt.CI = startCI + (sI - startI)
			return tkn
		}
	}
}

// Zig character literal: `'x'`, `'\n'`, `'\x41'`, `'\u{1F600}'`.
// Produces a numeric code point (if charAsNumber) or a one-char string.
func buildZonCharMatcher(charAsNumber bool) jsonic.MakeLexMatcher {
	return func(_ *jsonic.LexConfig, _ *jsonic.Options) jsonic.LexMatcher {
		return func(lex *jsonic.Lex, _ *jsonic.Rule) *jsonic.Token {
			pnt := lex.Cursor()
			src := lex.Src
			sI := pnt.SI
			if sI >= len(src) || src[sI] != '\'' {
				return nil
			}

			i := sI + 1
			if i >= len(src) {
				return nil
			}

			var codepoint int

			if src[i] == '\\' {
				i++
				if i >= len(src) {
					return nil
				}
				switch src[i] {
				case 'n':
					codepoint = '\n'
					i++
				case 'r':
					codepoint = '\r'
					i++
				case 't':
					codepoint = '\t'
					i++
				case '\\':
					codepoint = '\\'
					i++
				case '\'':
					codepoint = '\''
					i++
				case '"':
					codepoint = '"'
					i++
				case '0':
					codepoint = 0
					i++
				case 'x':
					i++
					if i+2 > len(src) {
						return nil
					}
					hex := src[i : i+2]
					if !isHex(hex) {
						return nil
					}
					n, err := strconv.ParseInt(hex, 16, 32)
					if err != nil {
						return nil
					}
					codepoint = int(n)
					i += 2
				case 'u':
					i++
					if i >= len(src) || src[i] != '{' {
						return nil
					}
					i++
					end := strings.IndexByte(src[i:], '}')
					if end < 0 {
						return nil
					}
					end += i
					hex := src[i:end]
					if !isHex(hex) {
						return nil
					}
					n, err := strconv.ParseInt(hex, 16, 32)
					if err != nil {
						return nil
					}
					codepoint = int(n)
					i = end + 1
				default:
					return nil
				}
			} else if src[i] != '\'' {
				r, size := utf8.DecodeRuneInString(src[i:])
				if r == utf8.RuneError && size <= 1 {
					return nil
				}
				codepoint = int(r)
				i += size
			} else {
				return nil
			}

			if i >= len(src) || src[i] != '\'' {
				return nil
			}
			i++

			var val any
			if charAsNumber {
				val = float64(codepoint)
			} else {
				val = string(rune(codepoint))
			}
			tsrc := src[sI:i]
			tkn := lex.Token("#NR", jsonic.TinNR, val, tsrc)
			pnt.SI = i
			pnt.CI += i - sI
			return tkn
		}
	}
}

func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// parseGrammarText parses grammar text into a GrammarSpec with refs attached.
func parseGrammarText(text string, refs map[jsonic.FuncRef]any) (*jsonic.GrammarSpec, error) {
	parsed, err := jsonic.Make().Parse(text)
	if err != nil {
		return nil, fmt.Errorf("zon: failed to parse grammar text: %w", err)
	}
	parsedMap, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("zon: grammar text did not parse to a map")
	}
	gs := &jsonic.GrammarSpec{Ref: refs}
	// Hand the declarative options block (if present) to jsonic via
	// OptionsMap; MapToOptions inside j.Grammar() converts it to a typed
	// Options value.
	if optsMap, ok := parsedMap["options"].(map[string]any); ok {
		gs.OptionsMap = optsMap
	}
	ruleMap, ok := parsedMap["rule"].(map[string]any)
	if !ok {
		return gs, nil
	}
	gs.Rule = make(map[string]*jsonic.GrammarRuleSpec, len(ruleMap))
	for name, rDef := range ruleMap {
		rd, ok := rDef.(map[string]any)
		if !ok {
			continue
		}
		grs := &jsonic.GrammarRuleSpec{}
		if openDef, ok := rd["open"]; ok {
			grs.Open = buildGrammarAltsOrSpec(openDef)
		}
		if closeDef, ok := rd["close"]; ok {
			grs.Close = buildGrammarAltsOrSpec(closeDef)
		}
		gs.Rule[name] = grs
	}
	return gs, nil
}

// buildGrammarAltsOrSpec accepts either a plain []any of alt maps (the
// legacy form) or a map with `alts: [...]` and optional `inject: {...}`
// modifiers (append, delete, move) — matching jsonic's internal
// parseGrammarAltsOrSpec helper.
func buildGrammarAltsOrSpec(def any) any {
	if arr, ok := def.([]any); ok {
		return buildGrammarAlts(arr)
	}
	m, ok := def.(map[string]any)
	if !ok {
		return nil
	}
	altsRaw, hasAlts := m["alts"].([]any)
	if !hasAlts {
		return nil
	}
	spec := &jsonic.GrammarAltListSpec{Alts: buildGrammarAlts(altsRaw)}
	if injectRaw, ok := m["inject"].(map[string]any); ok {
		inj := &jsonic.GrammarInjectSpec{}
		if appendV, ok := injectRaw["append"].(bool); ok {
			inj.Append = appendV
		}
		if del, ok := injectRaw["delete"].([]any); ok {
			for _, d := range del {
				if f, ok := d.(float64); ok {
					inj.Delete = append(inj.Delete, int(f))
				} else if i, ok := d.(int); ok {
					inj.Delete = append(inj.Delete, i)
				}
			}
		}
		if mv, ok := injectRaw["move"].([]any); ok {
			for _, v := range mv {
				if f, ok := v.(float64); ok {
					inj.Move = append(inj.Move, int(f))
				} else if i, ok := v.(int); ok {
					inj.Move = append(inj.Move, i)
				}
			}
		}
		spec.Inject = inj
	}
	return spec
}

// buildGrammarAlts converts a parsed-jsonic alt array into []*GrammarAltSpec.
func buildGrammarAlts(arr []any) []*jsonic.GrammarAltSpec {
	if arr == nil {
		return nil
	}
	alts := make([]*jsonic.GrammarAltSpec, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			alts = append(alts, &jsonic.GrammarAltSpec{})
			continue
		}
		ga := &jsonic.GrammarAltSpec{}
		if s, ok := m["s"]; ok {
			switch sv := s.(type) {
			case string:
				ga.S = sv
			case []any:
				strs := make([]string, len(sv))
				for i, v := range sv {
					strs[i], _ = v.(string)
				}
				ga.S = strs
			}
		}
		if b, ok := m["b"]; ok {
			switch bv := b.(type) {
			case float64:
				ga.B = int(bv)
			case int:
				ga.B = bv
			}
		}
		if p, ok := m["p"].(string); ok {
			ga.P = p
		}
		if r, ok := m["r"].(string); ok {
			ga.R = r
		}
		if a, ok := m["a"].(string); ok {
			ga.A = jsonic.FuncRef(a)
		}
		if c, ok := m["c"]; ok {
			switch cv := c.(type) {
			case string:
				ga.C = cv
			case map[string]any:
				ga.C = cv
			}
		}
		if u, ok := m["u"].(map[string]any); ok {
			ga.U = u
		}
		if g, ok := m["g"].(string); ok {
			ga.G = g
		}
		alts = append(alts, ga)
	}
	return alts
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func boolPtr(b bool) *bool {
	return &b
}
