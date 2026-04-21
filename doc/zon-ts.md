# ZON plugin for Jsonic (TypeScript)

A Jsonic syntax plugin that parses
[Zig Object Notation (ZON)](https://ziglang.org/documentation/master/#ZON)
into JavaScript values, with support for anonymous struct literals,
tuples, enum literals, numeric bases, character literals, multi-line
strings, and trailing commas.

```bash
npm install @jsonic/zon
```

Requires `jsonic` >= 2 as a peer dependency.


## Tutorials

### Parse a basic ZON document

Register the plugin and parse a top-level struct literal:

```typescript
import { Jsonic } from 'jsonic'
import { Zon } from '@jsonic/zon'

const j = Jsonic.make().use(Zon)

j('.{ .name = "Alice", .age = 30 }')
// { name: 'Alice', age: 30 }

j('.{ 1, 2, 3 }')
// [1, 2, 3]
```

### Parse a realistic build.zig.zon

ZON files typically have nested structs mixed with tuple-style
`paths` lists:

```typescript
import { Jsonic } from 'jsonic'
import { Zon } from '@jsonic/zon'

const j = Jsonic.make().use(Zon)

j(`.{
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
}`)
// {
//   name: 'example',
//   version: '0.0.1',
//   minimum_zig_version: '0.14.0',
//   dependencies: { foo: { url: '...', hash: '1220deadbeef' } },
//   paths: ['build.zig', 'src'],
// }
```

### Parse numbers in every ZON base

ZON numbers accept hex, octal, binary, and `_` separators:

```typescript
const j = Jsonic.make().use(Zon)

j('0x2a')      // 42
j('0o52')      // 42
j('0b101010')  // 42
j('1_000_000') // 1000000
j('3.14')      // 3.14
```


## How-to guides

### Parse character literals as code points

By default Zig char literals (`'A'`, `'\n'`, `'\u{1F600}'`) parse as
one-character strings. Set `charAsNumber: true` to receive numeric
code points instead:

```typescript
const j = Jsonic.make().use(Zon, { charAsNumber: true })

j("'A'")         // 65
j("'\\n'")       // 10
j("'\\u{1F600}'") // 128512
```

### Tag enum literals to distinguish them from strings

Without options, an enum literal value like `.red` becomes the plain
string `'red'`. If you need to tell it apart from an ordinary string
in the parsed tree, set `enumTag`:

```typescript
const j = Jsonic.make().use(Zon, { enumTag: '$enum' })

j('.{ .kind = .red, .label = "red" }')
// { kind: { $enum: 'red' }, label: 'red' }
```

### Read multi-line Zig strings

Consecutive lines prefixed with `\\` become a single string joined by
`\n`:

```typescript
const j = Jsonic.make().use(Zon)

j(`.{
  .description =
    \\\\first line
    \\\\second line
  ,
}`)
// { description: 'first line\nsecond line' }
```

### Reject extra alternates contributed by this plugin

Every grammar alternate added by the plugin carries the group tag
`zon`. To re-enable strict JSON while the plugin is loaded (rarely
useful, but supported), exclude that tag:

```typescript
const j = Jsonic.make().use(Zon).options({
  rule: { exclude: 'zon' },
})
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
     string or a numeric code point (`charAsNumber`).

All of the above are applied atomically through the `GrammarSpec`
passed to `jsonic.grammar(grammarDef, { rule: { alt: { g: 'zon' } } })`,
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
wraps it in `{ [enumTag]: name }` when the `enumTag` option is set).
The pair rule requires the trailing `#CL`, so the same `.foo` can't
be mistaken for a pair opener without an `=` following.


## Reference

### `Zon` (Plugin)

The plugin function. Register with `Jsonic.make().use(Zon, options)`.
`Zon.defaults` holds the merged default options.

### `ZonOptions`

```typescript
type ZonOptions = {
  // When true, parse Zig char literals ('x') as numeric code points.
  // When false (default), parse them as one-character strings.
  charAsNumber: boolean

  // When set, wrap enum literals (.foo used as value) in
  // `{ [enumTag]: name }` objects instead of producing bare strings.
  enumTag: null | string
}
```

Defaults:

```typescript
{
  charAsNumber: false,
  enumTag: null,
}
```

### Supported ZON syntax

| Construct            | Example                          | Result                   |
| -------------------- | -------------------------------- | ------------------------ |
| Struct literal       | `.{ .a = 1, .b = 2 }`            | `{ a: 1, b: 2 }`         |
| Empty struct literal | `.{}`                            | `[]` (empty list)        |
| Tuple literal        | `.{ 1, 2, 3 }`                   | `[1, 2, 3]`              |
| Nested               | `.{ .a = .{ .b = 1 } }`          | `{ a: { b: 1 } }`        |
| String               | `"hello\nworld"`                 | `'hello\nworld'`         |
| Multi-line string    | `\\line1\n\\line2`               | `'line1\nline2'`         |
| Number               | `42`, `0x2a`, `0o52`, `0b101010` | `42`                     |
| Number separator     | `1_000_000`                      | `1000000`                |
| Float                | `3.14`                           | `3.14`                   |
| Boolean / null       | `true`, `false`, `null`          | `true`, `false`, `null`  |
| Char literal         | `'A'`                            | `'A'` (or `65`)          |
| Enum literal         | `.red`                           | `'red'`                  |
| Trailing comma       | `.{ .a = 1, }`                   | `{ a: 1 }`               |
| Line comment         | `// ...`                         | *(ignored)*              |

### Grammar group tags

All grammar alternates added by the plugin carry the group tag
`zon`, so callers may exclude them via `rule.exclude: 'zon'`.
