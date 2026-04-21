/* Copyright (c) 2025 Richard Rodger, MIT License */

// Import Jsonic types used by plugins.
import {
  Jsonic,
  Rule,
  Plugin,
  Context,
  Config,
  Options,
  Lex,
} from 'jsonic'

// Plugin options.
type ZonOptions = {
  // When true, parse Zig char literals ('x') as numeric code points.
  // When false, parse them as single-character strings.
  charAsNumber: boolean
  // When set, wrap enum literals (.foo used as value) in `{ [enumTag]: 'foo' }`
  // instead of producing the bare string 'foo'.
  enumTag: null | string
}

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
#   - zonMultiString: consecutive \\\\-prefixed lines.
#   - zonChar:        'x', '\\n', '\\xNN', '\\u{...}' literals.
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
        n: '\\n'
        r: '\\r'
        t: '\\t'
        '\\\\': '\\\\'
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

// Plugin implementation.
const Zon: Plugin = (jsonic: Jsonic, options: ZonOptions) => {
  const charAsNumber = !!options.charAsNumber
  const enumTag = options.enumTag || null

  // Grammar actions:
  //   @pairkey  - capture the identifier at r.o[1] as the pair key
  //               (the 3-token 'DT TX CL' match puts the name in slot 1).
  //   @enum-val - produce the value for a `.name` enum literal appearing
  //               as a value; wrap in { [enumTag]: name } when configured.
  const refs: Record<string, Function> = {
    '@pairkey': (r: Rule) => {
      const tkn: any = r.o[1]
      r.u.key = tkn && tkn.val !== undefined ? tkn.val : tkn && tkn.src
    },
    '@enum-val': (r: Rule) => {
      const tkn: any = r.o[1]
      const name: string = tkn && (tkn.val as string)
      r.node = enumTag ? { [enumTag]: name } : name
    },
  }

  const grammarDef = Jsonic.make()(grammarText)
  grammarDef.ref = refs
  // All option overrides that can be expressed declaratively live inside
  // the grammar text (see zon-grammar.jsonic). The two that cannot are
  // applied here:
  //   - fixed.token  registers `.` as #DT and removes `[`/`]`
  //   - lex.match    holds matcher closures that capture plugin options
  grammarDef.options = {
    fixed: {
      token: {
        // Register `.` as its own token so the grammar can match
        // `#DT #OB`, `#DT #TX`, etc. via multi-token alt lookahead.
        '#DT': '.',
        // Bare `[`/`]` are not valid in ZON.
        '#OS': null,
        '#CS': null,
        // `=` replaces `:` as the key/value separator.
        '#CL': '=',
      },
    },
    lex: {
      match: {
        zonMultiString: { order: 1.1e5, make: buildZonMultiStringMatcher() },
        zonChar: { order: 1.2e5, make: buildZonCharMatcher(charAsNumber) },
      },
    },
  }

  // Tag every alt in this grammar with the 'zon' group so callers can
  // selectively exclude zon alts via `rule.exclude: 'zon'`.
  jsonic.grammar(grammarDef, { rule: { alt: { g: 'zon' } } })
}

// Multi-line Zig strings: consecutive lines starting with `\\`.
// Each `\\` line contributes its content verbatim (after the `\\`); lines
// are joined with `\n`.
function buildZonMultiStringMatcher() {
  return function makeZonMultiStringMatcher(cfg: Config, _opts: Options) {
    return function zonMultiStringMatcher(lex: Lex) {
      const { pnt } = lex; const src: string = lex.src as unknown as string
      if ('\\' !== src[pnt.sI] || '\\' !== src[pnt.sI + 1]) return undefined

      const startI = pnt.sI
      const startCI = pnt.cI
      let sI = pnt.sI
      let rI = pnt.rI
      const parts: string[] = []

      while ('\\' === src[sI] && '\\' === src[sI + 1]) {
        sI += 2
        const lineStart = sI
        while (sI < src.length && !cfg.line.chars[src[sI]]) sI++
        parts.push(src.substring(lineStart, sI))

        // Consume line terminator (handle \r\n as one).
        if (sI < src.length && cfg.line.chars[src[sI]]) {
          const ch = src[sI]
          if (cfg.line.rowChars[ch]) rI++
          sI++
          if (sI < src.length && '\r' === ch && '\n' === src[sI]) sI++
        }

        // Look for another `\\` continuation after inter-line whitespace.
        let peek = sI
        while (peek < src.length && (src[peek] === ' ' || src[peek] === '\t')) {
          peek++
        }
        if ('\\' !== src[peek] || '\\' !== src[peek + 1]) break
        sI = peek
      }

      const val = parts.join('\n')
      const tsrc = src.substring(startI, sI)
      const tkn = lex.token('#ST', val, tsrc, pnt)
      pnt.sI = sI
      pnt.rI = rI
      pnt.cI = startCI + (sI - startI)
      return tkn
    }
  }
}

// Zig character literal: `'x'`, `'\n'`, `'\x41'`, `'\u{1F600}'`.
// Produces a numeric code point (if charAsNumber) or a one-char string.
function buildZonCharMatcher(charAsNumber: boolean) {
  return function makeZonCharMatcher(_cfg: Config, _opts: Options) {
    return function zonCharMatcher(lex: Lex) {
      const { pnt } = lex; const src: string = lex.src as unknown as string
      const { sI, cI } = pnt
      if ('\'' !== src[sI]) return undefined

      let i = sI + 1
      let codepoint: number | null = null

      if ('\\' === src[i]) {
        i++
        const esc = src[i]
        switch (esc) {
          case 'n': codepoint = 10; i++; break
          case 'r': codepoint = 13; i++; break
          case 't': codepoint = 9; i++; break
          case '\\': codepoint = 92; i++; break
          case '\'': codepoint = 39; i++; break
          case '"': codepoint = 34; i++; break
          case '0': codepoint = 0; i++; break
          case 'x': {
            i++
            const hex = src.substring(i, i + 2)
            if (!/^[0-9a-fA-F]{2}$/.test(hex)) return undefined
            codepoint = parseInt(hex, 16)
            i += 2
            break
          }
          case 'u': {
            i++
            if ('{' !== src[i]) return undefined
            i++
            const endI = src.indexOf('}', i)
            if (-1 === endI) return undefined
            const hex = src.substring(i, endI)
            if (!/^[0-9a-fA-F]+$/.test(hex)) return undefined
            codepoint = parseInt(hex, 16)
            i = endI + 1
            break
          }
          default:
            return undefined
        }
      } else if (src[i] && '\'' !== src[i]) {
        codepoint = src.codePointAt(i) as number
        i += codepoint > 0xffff ? 2 : 1
      } else {
        return undefined
      }

      if ('\'' !== src[i]) return undefined
      i++

      const tsrc = src.substring(sI, i)
      const val = charAsNumber ? codepoint : String.fromCodePoint(codepoint!)
      const tkn = lex.token('#NR', val, tsrc, pnt)
      pnt.sI = i
      pnt.cI = cI + (i - sI)
      return tkn
    }
  }
}

// Default option values.
Zon.defaults = {
  charAsNumber: false,
  enumTag: null,
} as ZonOptions

export { Zon }
export type { ZonOptions }
