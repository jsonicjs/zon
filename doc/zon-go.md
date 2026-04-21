# ZON plugin for Jsonic (Go)

A Jsonic syntax plugin that parses
[Zig Object Notation (ZON)](https://ziglang.org/documentation/master/#ZON)
into Go values, with support for anonymous struct literals, tuples,
enum literals, numeric bases, character literals, multi-line strings,
and trailing commas.

```go
import (
  jsonic "github.com/jsonicjs/jsonic/go"
  zon "github.com/jsonicjs/zon/go"
)
```

```bash
go get github.com/jsonicjs/zon/go@latest
```


## Tutorials

### Parse a basic ZON document

Use the `Parse` convenience function to parse a top-level struct or
tuple literal:

```go
result, err := zon.Parse(`.{ .name = "Alice", .age = 30 }`)
// map[string]any{"name": "Alice", "age": float64(30)}

result, err = zon.Parse(`.{ 1, 2, 3 }`)
// []any{float64(1), float64(2), float64(3)}
```

### Parse a realistic build.zig.zon

ZON files typically have nested structs mixed with tuple-style
`paths` lists:

```go
src := `.{
    .name = "example",
    .version = "0.0.1",
    .minimum_zig_version = "0.14.0",
    .dependencies = .{
        .foo = .{
            .url = "https://example.com/foo.tar.gz",
            .hash = "1220deadbeef",
        },
    },
    .paths = .{
        "build.zig",
        "src",
    },
}`

result, err := zon.Parse(src)
// map[string]any{
//   "name":                "example",
//   "version":             "0.0.1",
//   "minimum_zig_version": "0.14.0",
//   "dependencies": map[string]any{
//     "foo": map[string]any{
//       "url": "https://example.com/foo.tar.gz", "hash": "1220deadbeef",
//     },
//   },
//   "paths": []any{"build.zig", "src"},
// }
```

### Parse numbers in every ZON base

ZON numbers accept hex, octal, binary, and `_` separators:

```go
zon.Parse("0x2a")      // float64(42)
zon.Parse("0o52")      // float64(42)
zon.Parse("0b101010")  // float64(42)
zon.Parse("1_000_000") // float64(1000000)
zon.Parse("3.14")      // float64(3.14)
```


## How-to guides

### Parse character literals as code points

By default Zig char literals (`'A'`, `'\n'`, `'\u{1F600}'`) parse as
one-character strings. Set `CharAsNumber` to receive numeric code
points instead:

```go
charAsNum := true
result, err := zon.Parse(`'A'`, zon.ZonOptions{CharAsNumber: &charAsNum})
// float64(65)
```

### Tag enum literals to distinguish them from strings

Without options, an enum literal value like `.red` becomes the plain
string `"red"`. If you need to tell it apart from an ordinary string
in the parsed tree, set `EnumTag`:

```go
result, err := zon.Parse(
  `.{ .kind = .red, .label = "red" }`,
  zon.ZonOptions{EnumTag: "$enum"},
)
// map[string]any{
//   "kind":  map[string]any{"$enum": "red"},
//   "label": "red",
// }
```

### Read multi-line Zig strings

Consecutive lines prefixed with `\\` become a single string joined
by `\n`:

```go
src := ".{\n" +
  "    .description =\n" +
  "        \\\\first line\n" +
  "        \\\\second line\n" +
  "    ,\n" +
  "}"

result, err := zon.Parse(src)
// map[string]any{"description": "first line\nsecond line"}
```

### Reuse a parser for many inputs

`Parse` rebuilds a Jsonic instance on every call. For hot paths, cache
an instance with `MakeJsonic` and reuse it:

```go
j := zon.MakeJsonic()
for _, src := range inputs {
    result, err := j.Parse(src)
    _ = result
    _ = err
}
```

### Reject extra alternates contributed by this plugin

Every grammar alternate added by the plugin carries the group tag
`zon`. To re-enable strict JSON while the plugin is loaded, exclude
that tag:

```go
j := jsonic.Make()
j.UseDefaults(zon.Zon, zon.Defaults)
j.SetOptions(jsonic.Options{Rule: &jsonic.RuleOptions{Exclude: "zon"}})
```


## Explanation

### How ZON parsing works

ZON is not a superset of JSON — it uses a distinct opening syntax
(`.{`), a different key/value separator (`=`), and key identifiers
prefixed with `.`. The plugin reshapes Jsonic into a ZON parser by
combining four mechanisms:

1. **Token remapping**: `.` is registered as a new `#DT` token, `=`
   replaces `:` as `#CL`, and `[`, `]` drop their default mappings so
   stray brackets produce a syntax error. `{` and `}` keep their
   defaults (`#OB`/`#CB`).

2. **Multi-token grammar lookahead**: rather than a custom lex
   matcher for `.{` / `.identifier`, the grammar uses jsonic's
   N-token alt lookahead to recognise struct and tuple literals
   directly from the token stream:

   - `#DT #OB #DT #TX #CL` → struct literal (push `map` rule)
   - `#DT #OB #CB`         → empty tuple literal
   - `#DT #OB`             → non-empty tuple literal (push `list`)
   - `#DT #TX`             → enum literal used as a value

   Inside the rules, `#DT #TX #CL` introduces a pair and `#CA #CB`
   absorbs trailing commas.

3. **Token-set restriction**: `KEY` is narrowed to `[#TX]` so only
   identifiers can appear on the left of `=`; `VAL` is narrowed to
   `[#NR, #ST, #VL]` so a bare identifier cannot substitute for a
   value (enum literals must be written `.name`).

4. **Plugin-only lex matchers** (things the default lexer can't
   express):
   - `zonMultiString`: consecutive `\\`-prefixed lines merged into a
     single `#ST` token.
   - `zonChar`: Zig char literals (`'x'`, `'\n'`, `'\xNN'`,
     `'\u{...}'`) emitted as `#NR` tokens whose value is a one-char
     string or a numeric code point (`CharAsNumber`).

All of the above are applied atomically through the `GrammarSpec`
passed to `j.Grammar(gs, &jsonic.GrammarSetting{...G: "zon"})`,
which tags every ZON alt with the `zon` group.

### Struct vs tuple disambiguation

ZON uses the same `.{ ... }` opener for both struct literals (with
`.field = value` pairs) and tuple literals (bare values). The five
positions of the `val` rule's first alt (`#DT #OB #DT #TX #CL`) give
the parser enough lookahead to commit to the struct branch only when
the actual tokens `.{.ident =` are present; anything else falls
through to the `#DT #OB` tuple branch.

### Enum literals as values

A bare `.foo` token sequence (`#DT #TX`) is handled by a dedicated
`val` alt whose action sets the node to the identifier string (or
wraps it in `map[string]any{enumTag: name}` when the `EnumTag`
option is set). The pair rule requires a trailing `#CL`, so the same
`.foo` can't be mistaken for a pair opener without an `=` following.


## Reference

### `Parse(src string, opts ...ZonOptions) (any, error)`

Parses a ZON string and returns the resulting value. Convenience
wrapper around `MakeJsonic(opts...).Parse(src)`.

### `MakeJsonic(opts ...ZonOptions) *jsonic.Jsonic`

Returns a reusable Jsonic instance configured for ZON parsing. Use
this when parsing multiple ZON strings with the same options.

### `Zon(j *jsonic.Jsonic, options map[string]any) error`

The raw plugin function. Usually called indirectly through
`j.UseDefaults(zon.Zon, zon.Defaults, opts...)` or via the `Parse`
and `MakeJsonic` helpers above.

### `Defaults`

```go
var Defaults = map[string]any{
    "charAsNumber": false,
    "enumTag":      "",
}
```

### `ZonOptions`

```go
type ZonOptions struct {
    // When non-nil and true, parses Zig char literals ('x') as numeric
    // code points. When nil or false (default), they are one-char strings.
    CharAsNumber *bool

    // When non-empty, wraps enum literals (.foo used as value) in
    // map[string]any{<EnumTag>: name} instead of producing bare strings.
    EnumTag string
}
```

### Supported ZON syntax

| Construct            | Example                          | Result                         |
| -------------------- | -------------------------------- | ------------------------------ |
| Struct literal       | `.{ .a = 1, .b = 2 }`            | `map[string]any{"a":1,"b":2}`  |
| Empty struct literal | `.{}`                            | `[]any{}` (empty list)         |
| Tuple literal        | `.{ 1, 2, 3 }`                   | `[]any{1, 2, 3}`               |
| Nested               | `.{ .a = .{ .b = 1 } }`          | nested maps                    |
| String               | `"hello\nworld"`                 | `"hello\nworld"`               |
| Multi-line string    | `\\line1\n\\line2`               | `"line1\nline2"`               |
| Number               | `42`, `0x2a`, `0o52`, `0b101010` | `float64(42)`                  |
| Number separator     | `1_000_000`                      | `float64(1000000)`             |
| Float                | `3.14`                           | `float64(3.14)`                |
| Boolean / null       | `true`, `false`, `null`          | `true`, `false`, `nil`         |
| Char literal         | `'A'`                            | `"A"` (or `float64(65)`)       |
| Enum literal         | `.red`                           | `"red"`                        |
| Trailing comma       | `.{ .a = 1, }`                   | `map[string]any{"a":1}`        |
| Line comment         | `// ...`                         | *(ignored)*                    |

### Grammar group tags

All grammar alternates added by the plugin carry the group tag
`zon`, so callers may exclude them via
`Options{Rule: &RuleOptions{Exclude: "zon"}}`.
